#!/bin/bash
# CLI dispatcher. Sourced by ./run after all run-<name> functions are defined.
# Auto-discovers every function whose name begins with `run-` and exposes it
# as a subcommand: `./run <name> [args]` -> `run-<name> "$@"`.
set -eo pipefail

# Shared logging / color helpers.
source "$(dirname "${BASH_SOURCE[0]}")/_lib.sh"

# ============================================================================
# Help
# ============================================================================

show-help() {
    items=()
    while IFS='' read -r line; do items+=("$line"); done < \
        <(compgen -A "function" | grep "^run-" | sed "s/^run-//")
    printf -v items "\t%s\n" "${items[@]}"

    usage="$(p-green "USAGE:") $(basename "$0") CMD [ARGUMENTS]
  $(p-blue "CMD:")\n$items"
    printf "$usage"
}

# ============================================================================
# Dispatch
# ============================================================================

name=$1
case "$name" in
    "" | "-h" | "--help" | "help")
        show-help
        ;;
    *)
        shift
        if compgen -A "function" | grep -qx "run-$name" ; then
            run-"${name}" "$@"
        else
            p-error "run-$name not found."
            echo ""
            show-help
            exit 123
        fi
        ;;
esac
