FROM python:3.12-alpine

WORKDIR /app

# Copy version file to make builds observable
COPY version.txt /app/version.txt

# Simple HTTP server that responds with version and env vars
RUN cat > server.py << 'EOF'
import os
from http.server import HTTPServer, BaseHTTPRequestHandler

class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/health":
            self.send_response(200)
            self.end_headers()
            self.wfile.write(b"ok")
        elif self.path == "/version":
            self.send_response(200)
            self.end_headers()
            with open("version.txt") as f:
                self.wfile.write(f.read().encode())
        elif self.path == "/env":
            self.send_response(200)
            self.end_headers()
            env_info = f"STEVEDORE_DEPLOYMENT={os.environ.get('STEVEDORE_DEPLOYMENT', 'unset')}\n"
            self.wfile.write(env_info.encode())
        else:
            self.send_response(200)
            self.end_headers()
            self.wfile.write(b"simple-app running")

    def log_message(self, format, *args):
        pass  # Suppress access logs

HTTPServer(("", 8080), Handler).serve_forever()
EOF

EXPOSE 8080
CMD ["python", "server.py"]
