#!/usr/bin/env bash

# Wipes the local `cs` installation (tmux sessions, state, worktrees, binary)
# and reinstalls it from the code next to this script.

set -e

err() {
    echo "$1" >&2
    exit 1
}

ensure() {
    if ! "$@"; then err "command failed: $*"; fi
}

kill_sessions() {
    command -v tmux &> /dev/null || return 0
    local sessions
    sessions=$(tmux ls 2>/dev/null | grep '^claudesquad_' | cut -d: -f1 || true)
    [[ -n "$sessions" ]] || return 0

    echo "Killing tmux sessions:"
    echo "$sessions" | sed 's/^/  /'
    echo "$sessions" | xargs -r -n1 tmux kill-session -t
}

main() {
    INSTALL_NAME="cs"
    BIN_DIR=${BIN_DIR:-$HOME/.local/bin}
    CONFIG_DIR="$HOME/.claude-squad"
    KEEP_CONFIG=0
    ASSUME_YES=0

    while [[ $# -gt 0 ]]; do
        case $1 in
            --name)
                [[ -n "${2:-}" ]] || err "--name needs a value"
                INSTALL_NAME="$2"
                shift 2
                ;;
            --keep-config)
                KEEP_CONFIG=1
                shift
                ;;
            -y|--yes)
                ASSUME_YES=1
                shift
                ;;
            *)
                echo "Unknown option: $1"
                echo "Usage: restart.sh [--name <name>] [--keep-config] [-y]    (BIN_DIR=<dir> to change where it lands)"
                exit 1
                ;;
        esac
    done

    SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

    echo "This will permanently delete:"
    echo "  - every running tmux session named claudesquad_*"
    if [[ $KEEP_CONFIG -eq 1 ]]; then
        echo "  - ${CONFIG_DIR}/worktrees and ${CONFIG_DIR}/state.json (config.json kept)"
    else
        echo "  - ${CONFIG_DIR} (config, state and ALL worktrees)"
    fi
    echo "  - ${BIN_DIR}/${INSTALL_NAME}"
    echo ""
    echo "Uncommitted work inside those worktrees is lost and cannot be recovered."
    echo ""

    if [[ $ASSUME_YES -eq 0 ]]; then
        read -r -p "Type 'yes' to continue: " reply
        [[ "$reply" == "yes" ]] || err "Aborted."
    fi

    kill_sessions

    if [[ $KEEP_CONFIG -eq 1 ]]; then
        rm -rf "${CONFIG_DIR}/worktrees"
        rm -f "${CONFIG_DIR}"/state.json*
    else
        rm -rf "$CONFIG_DIR"
    fi

    rm -f "${BIN_DIR}/${INSTALL_NAME}"

    echo "Reinstalling from ${SRC_DIR}..."
    ensure "${SRC_DIR}/install.sh" --name "$INSTALL_NAME"
}

main "$@" || exit 1
