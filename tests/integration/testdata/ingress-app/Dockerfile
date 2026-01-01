FROM python:3.11-alpine

WORKDIR /app
COPY server.py .
COPY version.txt .

EXPOSE 8080

CMD ["python", "server.py"]
