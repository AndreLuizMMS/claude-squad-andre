#!/usr/bin/env bash

# Builds this repository and installs it as `cs`. This is a fork: there are no
# published releases to download, the binary comes from the code next to this
# script.

set -e

err() {
    echo "$1" >&2
    exit 1
}

ensure() {
    if ! "$@"; then err "command failed: $*"; fi
}

setup_shell_and_path() {
    if [[ ":$PATH:" == *":${BIN_DIR}:"* ]]; then
        return
    fi

    case $SHELL in
        */zsh)  PROFILE=$HOME/.zshrc ;;
        */bash) PROFILE=$HOME/.bashrc ;;
        */fish) PROFILE=$HOME/.config/fish/config.fish ;;
        */ash)  PROFILE=$HOME/.profile ;;
        *)
            echo "could not detect shell, manually add ${BIN_DIR} to your PATH."
            return
    esac

    echo >> "$PROFILE" && echo "export PATH=\"\$PATH:$BIN_DIR\"" >> "$PROFILE"
    echo "Added ${BIN_DIR} to your PATH in ${PROFILE}. Open a new shell to pick it up."
}

check_dependencies() {
    if ! command -v go &> /dev/null; then
        err "Go is not installed. See https://go.dev/dl/"
    fi

    if command -v tmux &> /dev/null; then
        return
    fi

    echo "tmux is not installed. Installing tmux..."
    case "$(uname | tr '[:upper:]' '[:lower:]')" in
        darwin)
            command -v brew &> /dev/null || err "Homebrew is required to install tmux. See https://brew.sh"
            ensure brew install tmux
            ;;
        linux)
            if command -v apt-get &> /dev/null; then
                ensure sudo apt-get update
                ensure sudo apt-get install -y tmux
            elif command -v dnf &> /dev/null; then
                ensure sudo dnf install -y tmux
            elif command -v yum &> /dev/null; then
                ensure sudo yum install -y tmux
            elif command -v pacman &> /dev/null; then
                ensure sudo pacman -S --noconfirm tmux
            else
                err "Could not determine the package manager. Please install tmux manually."
            fi
            ;;
        *)
            err "Please install tmux manually."
            ;;
    esac
}

main() {
    INSTALL_NAME="cs"
    BIN_DIR=${BIN_DIR:-$HOME/.local/bin}

    while [[ $# -gt 0 ]]; do
        case $1 in
            --name)
                [[ -n "${2:-}" ]] || err "--name needs a value"
                INSTALL_NAME="$2"
                shift 2
                ;;
            *)
                echo "Unknown option: $1"
                echo "Usage: install.sh [--name <name>]    (BIN_DIR=<dir> to change where it lands)"
                exit 1
                ;;
        esac
    done

    # Build from the directory holding this script, so it works from anywhere.
    SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

    check_dependencies

    mkdir -p "$BIN_DIR"

    echo "Building from ${SRC_DIR}..."
    ensure go build -C "$SRC_DIR" -o "${BIN_DIR}/${INSTALL_NAME}"

    setup_shell_and_path

    echo ""
    echo "Installed as '${INSTALL_NAME}' in ${BIN_DIR}:"
    echo "$("${BIN_DIR}/${INSTALL_NAME}" version)"
}

main "$@" || exit 1
