package store

import (
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "d"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// LWW：新改动胜出，旧改动被拒。
func TestSyncMergeLWW(t *testing.T) {
	s := newTestStore(t)
	s.SyncMerge([]Task{{ID: "t1", Title: "base", UpdatedAt: 100}}, nil, nil)
	// 旧改动不覆盖
	adopt, _ := s.SyncMerge([]Task{{ID: "t1", Title: "stale", UpdatedAt: 50}}, nil, nil)
	if adopt != 0 {
		t.Fatalf("旧改动被采纳了 adopt=%d", adopt)
	}
	// 新改动覆盖
	adopt, _ = s.SyncMerge([]Task{{ID: "t1", Title: "fresh", UpdatedAt: 200}}, nil, nil)
	if adopt != 1 {
		t.Fatalf("新改动未采纳 adopt=%d", adopt)
	}
	got := s.ListTasks()
	if len(got) != 1 || got[0].Title != "fresh" {
		t.Fatalf("结果不对: %+v", got)
	}
}

// 墓碑：删除压过更旧的更新；更新比墓碑新则复活。
func TestSyncMergeTombstone(t *testing.T) {
	s := newTestStore(t)
	s.SyncMerge([]Task{{ID: "t1", Title: "base", UpdatedAt: 100}}, nil, nil)
	// 删除（墓碑 150）
	s.SyncMerge(nil, nil, map[string]int64{"task:t1": 150})
	// 比墓碑旧的更新不得复活
	adopt, _ := s.SyncMerge([]Task{{ID: "t1", Title: "zombie", UpdatedAt: 120}}, nil, nil)
	if adopt != 0 {
		t.Fatalf("旧更新复活了 adopt=%d", adopt)
	}
	if len(s.ListTasks()) != 0 {
		t.Fatalf("墓碑未生效: %+v", s.ListTasks())
	}
	// 比墓碑新的更新可以复活
	adopt, _ = s.SyncMerge([]Task{{ID: "t1", Title: "revive", UpdatedAt: 300}}, nil, nil)
	if adopt != 1 {
		t.Fatalf("新更新未复活 adopt=%d", adopt)
	}
	if got := s.ListTasks(); len(got) != 1 || got[0].Title != "revive" {
		t.Fatalf("复活结果不对: %+v", got)
	}
}

// 服务端删除也产生墓碑，可随同步下发。
func TestDeleteWritesTombstone(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.AddTask(Task{Title: "x"})
	if err := s.DeleteTask(task.ID); err != nil {
		t.Fatal(err)
	}
	if ts, ok := s.TombstonesCopy()["task:"+task.ID]; !ok || ts <= 0 {
		t.Fatalf("删除未写墓碑: %+v", s.TombstonesCopy())
	}
}

// 活动按 ID 幂等（同步重放不重复计）。
func TestAddActivityIdempotent(t *testing.T) {
	s := newTestStore(t)
	s.AddActivity(Activity{ID: "a1", Kind: "upload", Title: "f"})
	s.AddActivity(Activity{ID: "a1", Kind: "upload", Title: "f"})
	if got := s.ListActivities(10); len(got) != 1 {
		t.Fatalf("幂等失效: %d 条", len(got))
	}
}

// 全账号聚合：成员自有 + 团队公共 + 服务端默认，带归属。
func TestAllIMAAccounts(t *testing.T) {
	s := newTestStore(t)
	u := &User{Username: "alice", IMAAccounts: []IMAAccount{{ID: "u1", Name: "alice的", ClientID: "c1", APIKey: "k1"}}}
	if err := s.AddUser(u); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddTeamAccount(IMAAccount{Name: "公共", ClientID: "c2", APIKey: "k2"}); err != nil {
		t.Fatal(err)
	}
	_ = s.UpdateSettings(func(st *Settings) error {
		st.DefaultIMAClientID = "c3"
		st.DefaultIMAAPIKey = "k3"
		return nil
	})
	all := s.AllIMAAccounts()
	if len(all) != 3 {
		t.Fatalf("数量不对: %d", len(all))
	}
	sawUser, sawTeam, sawDefault := false, false, false
	for _, a := range all {
		switch a.Source {
		case "user":
			sawUser = true
			if a.Owner != "alice" {
				t.Fatalf("归属不对: %+v", a)
			}
		case "team":
			sawTeam = true
		case "default":
			sawDefault = true
		}
	}
	if !sawUser || !sawTeam || !sawDefault {
		t.Fatalf("来源不全: %+v", all)
	}
}

// 整表替换（客户端拉取后以合并结果为准）。
func TestReplaceTasks(t *testing.T) {
	s := newTestStore(t)
	s.AddTask(Task{Title: "old"})
	s.ReplaceTasks([]Task{{ID: "n1", Title: "new", UpdatedAt: 1}})
	if got := s.ListTasks(); len(got) != 1 || got[0].ID != "n1" {
		t.Fatalf("替换失败: %+v", got)
	}
}
