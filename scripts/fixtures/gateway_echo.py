"""Synthetic upstream; CI only, never copied into production images."""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
import threading
import time

count = 0
lock = threading.Lock()


class Echo(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass  # Headers, cookies and request bodies must never reach artifacts.

    def do_HEAD(self):
        self.do_GET()

    def do_POST(self):
        self.do_GET()

    def do_GET(self):
        global count
        body = self.rfile.read(int(self.headers.get('Content-Length', '0')))
        with lock:
            if self.path.endswith('/disconnect'):
                count += 1
                print('accepted-disconnect', flush=True)
                self.close_connection = True
                return
            received = count
        if self.path.endswith('/timeout'):
            time.sleep(37)
        payload = json.dumps({'method': self.command, 'path': self.path, 'body': body.decode(),
                              'headers': dict(self.headers), 'received': received}).encode()
        if self.path.endswith('/large'):
            payload = b'x' * (16 * 1024 * 1024)
        status = 422 if self.path.endswith('/error') else 200
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(payload)))
        self.end_headers()
        if self.command != 'HEAD':
            try:
                self.wfile.write(payload)
            except (BrokenPipeError, ConnectionResetError):
                pass


ThreadingHTTPServer(('0.0.0.0', int(os.environ['PORT'])), Echo).serve_forever()
