FROM python:3.12-alpine

WORKDIR /app

# Copy version file to make builds observable
COPY version.txt /app/version.txt

# Simple HTTP server that responds with version and env vars
COPY server.py /app/server.py

EXPOSE 8080
CMD ["python", "server.py"]
