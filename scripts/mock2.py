#!/usr/bin/env python3
"""Mock ima 开放平台 + COS 接收端：验证压缩包自动解压上传全链路。"""
import json
import os
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer

COS_DIR = "/tmp/mock-cos"
os.makedirs(COS_DIR, exist_ok=True)
LOCK = threading.Lock()
STATS = {"create_media": 0, "add_knowledge": 0, "cos_put": 0, "names": []}


def knowledge_list(body):
    kb = body.get("knowledge_base_id", "")
    folder = body.get("folder_id", "")
    if kb == "kb1" and folder in ("", "kb1"):
        return {
            "knowledge_list": [
                {"folder_id": "f1", "name": "数学", "file_number": "2", "folder_number": "0", "parent_folder_id": "kb1"},
                {"media_id": "m_a", "title": "a.pdf", "parent_folder_id": "kb1"},
                {"media_id": "m_b", "title": "b.docx", "parent_folder_id": "kb1"},
                {"media_id": "m_c", "title": "c.png", "parent_folder_id": "kb1"},
            ],
            "is_end": True, "next_cursor": "", "current_path": [],
        }
    if kb == "kb1" and folder == "f1":
        return {
            "knowledge_list": [
                {"media_id": "m_d", "title": "d.md", "parent_folder_id": "f1"},
                {"media_id": "m_e", "title": "e.txt", "parent_folder_id": "f1"},
            ],
            "is_end": True, "next_cursor": "", "current_path": [],
        }
    return {"knowledge_list": [], "is_end": True, "next_cursor": "", "current_path": []}


MEDIA_NAMES = {}

def create_media(body):
    with LOCK:
        STATS["create_media"] += 1
        n = STATS["create_media"]
        MEDIA_NAMES["mock_media_%d" % n] = body.get("file_name", "")
    return {
        "media_id": "mock_media_%d" % n,
        "cos_credential": {
            "token": "mock-token", "secret_id": "mock-sid", "secret_key": "mock-skey",
            "start_time": 0, "expired_time": 4102444800,
            "bucket_name": "mockbucket", "region": "mock-1",
            "cos_key": "mock-key-%d" % n,
        },
    }


def add_knowledge(body):
    with LOCK:
        STATS["add_knowledge"] += 1
        STATS["names"].append(body.get("title", ""))
    return {"media_id": body.get("media_id", "")}


def get_media_info(body):
    mid = str(body.get("media_id", ""))
    name = MEDIA_NAMES.get(mid, "")
    extmap = {"pdf": 1, "docx": 3, "pptx": 4, "xlsx": 5, "md": 7, "png": 9,
              "jpg": 9, "jpeg": 9, "txt": 13, "xmind": 14, "mp3": 15,
              "html": 20, "htm": 20, "epub": 21}
    if name:
        mt = extmap.get(name.rsplit(".", 1)[-1].lower(), 1)
    else:
        mt = 7 if mid.endswith("d") else 1
    return {
        "media_type": mt,
        "url_info": {"url": "http://127.0.0.1:18094/file/" + mid, "headers": {}},
    }


ROUTES = {
    "/openapi/wiki/v1/search_knowledge_base": lambda b: {"info_list": [
        {"kb_id": "kb1", "kb_name": "测试知识库", "cover_url": "", "content_count": "2", "base_type": "个人知识库"},
        {"kb_id": "kb2", "kb_name": "未授权库", "cover_url": "", "content_count": "1", "base_type": "个人知识库"},
    ], "is_end": True, "next_cursor": ""},
    "/openapi/wiki/v1/get_knowledge_list": knowledge_list,
    "/openapi/wiki/v1/search_knowledge": lambda b: {
        "info_list": [
            {"media_id": "m_a", "title": "数学复习提纲.pdf", "parent_folder_id": "kb1",
             "highlight_content": "第一章 <em>数学</em> 极限与导数的核心考点整理…"},
            {"media_id": "m_d", "title": "d.md", "parent_folder_id": "f1",
             "highlight_content": "<em>数学</em>错题归档方法：按知识点分组…"},
        ],
        "is_end": True, "next_cursor": "",
    },
    "/openapi/wiki/v1/get_addable_knowledge_base_list": lambda b: {"addable_knowledge_base_list": [], "next_cursor": "", "is_end": True},
    "/openapi/wiki/v1/check_repeated_names": lambda b: {"results": [{"name": p.get("name", ""), "is_repeated": False} for p in b.get("params", [])]},
    "/openapi/wiki/v1/create_media": create_media,
    "/openapi/wiki/v1/add_knowledge": add_knowledge,
    "/openapi/wiki/v1/get_media_info": get_media_info,
    "/openapi/note/v1/import_doc": lambda b: {"note_id": "note_1"},
}


class ImaHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        # 文件内容 mock（供下载/预览代理拉取）
        if self.path.startswith("/file/"):
            fid = self.path.split("/file/", 1)[1]
            body = ("# mock 文件内容 %s\n\n这是 %s 的模拟正文，用于验证下载与预览链路。\n" % (fid, fid)).encode()
            self.send_response(200)
            self.send_header("Content-Type", "text/plain; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        else:
            self.send_response(404)
            self.end_headers()

    def do_POST(self):
        n = int(self.headers.get("Content-Length", 0))
        body = json.loads(self.rfile.read(n) or b"{}")
        fn = ROUTES.get(self.path)
        data = fn(body) if fn else {}
        resp = json.dumps({"code": 0, "msg": "ok", "data": data}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(resp)))
        self.end_headers()
        self.wfile.write(resp)

    def log_message(self, *a):
        pass


class CosHandler(BaseHTTPRequestHandler):
    def do_PUT(self):
        n = int(self.headers.get("Content-Length", 0))
        data = self.rfile.read(n)
        key = self.path.lstrip("/").replace("/", "_") or "unnamed"
        with open(os.path.join(COS_DIR, key), "wb") as f:
            f.write(data)
        with LOCK:
            STATS["cos_put"] += 1
        self.send_response(200)
        self.send_header("Content-Length", "0")
        self.end_headers()

    def do_GET(self):
        if self.path == "/_stats":
            resp = json.dumps(STATS, ensure_ascii=False).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(resp)))
            self.end_headers()
            self.wfile.write(resp)
        else:
            self.send_response(404)
            self.end_headers()

    def log_message(self, *a):
        pass


class LLMHandler(BaseHTTPRequestHandler):
    """OpenAI 兼容 /v1/chat/completions mock。"""

    def do_POST(self):
        n = int(self.headers.get("Content-Length", 0))
        body = json.loads(self.rfile.read(n) or b"{}")
        user_msg = ""
        for m in body.get("messages", []):
            if m.get("role") == "user":
                user_msg = m.get("content", "")
        answer = "【mock-llm 合成回答】根据知识库资料：数学复习的重点是极限与导数（资料[1]），错题按知识点分组归档（资料[2]）。"
        resp = json.dumps({
            "id": "mock", "object": "chat.completion", "model": body.get("model", "mock"),
            "choices": [{"index": 0, "message": {"role": "assistant", "content": answer}}],
        }).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(resp)))
        self.end_headers()
        self.wfile.write(resp)
        with LOCK:
            STATS.setdefault("llm_calls", 0)
            STATS["llm_calls"] += 1

    def log_message(self, *a):
        pass


if __name__ == "__main__":
    threading.Thread(target=lambda: HTTPServer(("127.0.0.1", 18093), CosHandler).serve_forever(), daemon=True).start()
    threading.Thread(target=lambda: HTTPServer(("127.0.0.1", 18095), LLMHandler).serve_forever(), daemon=True).start()
    HTTPServer(("127.0.0.1", 18094), ImaHandler).serve_forever()
