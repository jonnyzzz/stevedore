#!/usr/bin/env python3
"""Simple HTTP server for testing ingress labels."""

import http.server
import json
import os
import socketserver

PORT = 8080

class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == '/health':
            self.send_response(200)
            self.send_header('Content-type', 'text/plain')
            self.end_headers()
            self.wfile.write(b'OK')
        elif self.path == '/version':
            self.send_response(200)
            self.send_header('Content-type', 'text/plain')
            self.end_headers()
            try:
                with open('version.txt') as f:
                    version = f.read().strip()
            except Exception:
                version = 'unknown'
            self.wfile.write(version.encode())
        elif self.path == '/env':
            self.send_response(200)
            self.send_header('Content-type', 'application/json')
            self.end_headers()
            env_data = {k: v for k, v in os.environ.items() if k.startswith('STEVEDORE_')}
            self.wfile.write(json.dumps(env_data).encode())
        else:
            self.send_response(404)
            self.send_header('Content-type', 'text/plain')
            self.end_headers()
            self.wfile.write(b'Not Found')

    def log_message(self, format, *args):
        print(f"[server] {args[0]}")

if __name__ == '__main__':
    with socketserver.TCPServer(("", PORT), Handler) as httpd:
        print(f"Serving on port {PORT}")
        httpd.serve_forever()
