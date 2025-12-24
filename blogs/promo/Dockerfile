FROM python:3.9-slim

RUN apt-get update && apt-get install -y \
    curl unzip fontconfig \
    && rm -rf /var/lib/apt/lists/*

# Download JetBrains Mono
# Using a specific version to ensure reproducibility
RUN mkdir -p /usr/share/fonts/truetype/jetbrains && \
    curl -L -o /tmp/jbmono.zip https://github.com/JetBrains/JetBrainsMono/releases/download/v2.304/JetBrainsMono-2.304.zip && \
    unzip /tmp/jbmono.zip -d /tmp/jbmono && \
    cp /tmp/jbmono/fonts/ttf/*.ttf /usr/share/fonts/truetype/jetbrains/ && \
    rm -rf /tmp/jbmono* && \
    fc-cache -f -v

RUN pip install Pillow

WORKDIR /app
COPY generate_images.py .

CMD ["python", "generate_images.py"]