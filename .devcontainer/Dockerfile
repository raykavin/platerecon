# Imagem base do Go com Debian (Microsoft)
FROM mcr.microsoft.com/devcontainers/go:1-1.24-bookworm

ENV DEBIAN_FRONTEND=noninteractive

ARG USERNAME=vscode
ARG USER_UID=1000
ARG USER_GID=1000

RUN apt-get update && apt-get install -y \
    fish \
    sudo \
    git \
    curl \
    && apt-get clean && rm -rf /var/lib/apt/lists/*

RUN if ! getent group $USERNAME >/dev/null; then \
    groupadd --gid $USER_GID $USERNAME; \
    fi

RUN if ! id -u $USERNAME >/dev/null 2>&1; then \
    useradd --uid $USER_UID --gid $USER_GID -m $USERNAME; \
    fi \
    && usermod -aG sudo $USERNAME \
    && echo "$USERNAME ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/$USERNAME \
    && chmod 0440 /etc/sudoers.d/$USERNAME

RUN chsh -s /usr/bin/fish $USERNAME

WORKDIR /home/$USERNAME/workspaces/pon_watcher

USER $USERNAME

CMD ["fish"]
