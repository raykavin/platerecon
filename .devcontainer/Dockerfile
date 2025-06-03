# Base image
FROM mcr.microsoft.com/devcontainers/go:1-1.24-bookworm

ENV DEBIAN_FRONTEND=noninteractive
ARG USERNAME=vscode

# Install packages
RUN apt-get update && apt-get install -y --no-install-recommends \
    fish \
    git \
    curl \
    sudo \
    libtesseract-dev \
    libleptonica-dev \
    tesseract-ocr \
    && apt-get clean && rm -rf /var/lib/apt/lists/*

# Change shell to fish
RUN chsh -s /usr/bin/fish ${USERNAME}

# Use non-root user
USER ${USERNAME}
WORKDIR /home/${USERNAME}/workspaces/platerecon

# Clone and install GoCV
RUN git clone https://github.com/hybridgroup/gocv.git /tmp/gocv \
    && cd /tmp/gocv \
    && git checkout v0.41.0 \
    && make install \
    && rm -rf /tmp/gocv

# Start in fish shell
CMD ["fish"]
