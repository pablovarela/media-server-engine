import json
import os
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit

state_file, writes_log, port_file = sys.argv[1:4]
required_headers = json.loads(os.environ.get("FAKE_APP_HEADERS", "{}"))


def load():
    with open(state_file) as state:
        return json.load(state)


def save(state):
    with open(state_file, "w") as out:
        json.dump(state, out)


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def answer(self, code, body=None):
        payload = b"" if body is None else json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def authorised(self):
        return all(self.headers.get(key) == value for key, value in required_headers.items())

    def body(self):
        length = int(self.headers.get("Content-Length") or 0)
        return json.loads(self.rfile.read(length)) if length else None

    def collection_and_id(self, path, state):
        if path in state:
            return path, None
        parent, _, item_id = path.rpartition("/")
        if parent in state and isinstance(state[parent], list):
            return parent, item_id
        return None, None

    def do_GET(self):
        if not self.authorised():
            return self.answer(401)
        state = load()
        path = urlsplit(self.path).path
        collection, item_id = self.collection_and_id(path, state)
        if collection is None:
            return self.answer(404)
        if item_id is None:
            return self.answer(200, state[collection])
        item = next((i for i in state[collection] if str(i.get("id")) == item_id), None)
        return self.answer(200, item) if item else self.answer(404)

    def write(self, method):
        if not self.authorised():
            return self.answer(401)
        body = self.body()
        rejected = os.environ.get("FAKE_APP_REJECT")
        if rejected and rejected in json.dumps(body):
            return self.answer(400, [{"errorMessage": "rejected by the fake app"}])
        with open(writes_log, "a") as log:
            log.write(json.dumps({"method": method, "path": self.path, "body": body}) + "\n")
        state = load()
        path = urlsplit(self.path).path
        effect = state.get("effects", {}).get(f"{method} {path}")
        if effect is not None:
            return self.apply(effect, state)
        collection, item_id = self.collection_and_id(path, state)
        if collection is None or not isinstance(state[collection], list):
            state[path] = body
            save(state)
            return self.answer(200, body)
        items = state[collection]
        if method == "POST" and item_id is None and isinstance(body, dict):
            body = dict(body, id=max([i.get("id", 0) for i in items] + [0]) + 1)
            items.append(body)
        elif method == "PUT" and item_id is not None:
            state[collection] = [body if str(i.get("id")) == item_id else i for i in items]
        elif method == "DELETE":
            state[collection] = [i for i in items if str(i.get("id")) != item_id]
        save(state)
        return self.answer(200 if method != "POST" else 201, body)

    def apply(self, effect, state):
        if "append" in effect:
            target, key, item = effect["append"]
            (state[target][key] if key else state[target]).append(item)
        if "set" in effect:
            target, key, value = effect["set"]
            state[target][key] = value
        save(state)
        respond = effect.get("respond")
        return self.answer(200 if respond is not None else 204, respond)

    def do_POST(self):
        self.write("POST")

    def do_PUT(self):
        self.write("PUT")

    def do_DELETE(self):
        self.write("DELETE")


server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
with open(port_file + ".tmp", "w") as out:
    out.write(str(server.server_address[1]))
os.rename(port_file + ".tmp", port_file)
server.serve_forever()
