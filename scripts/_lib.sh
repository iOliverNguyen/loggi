#!/bin/bash
# Shared helpers for ./run subcommands — color/logging utilities and
# usage_prompt. Source this file; do NOT execute it directly.

# ============================================================================
# Color utilities
# ============================================================================

t-color()  { printf "\e[38;5;%dm" "$1" ; }
t-yellow() { t-color 3 ; }
t-reset()  { printf "\e[0m" ; }

p-color() {
    local color=$1 ; shift
    printf "\e[38;5;%dm" "$color"
    printf "$@"
    printf "\e[0m"
}

p-red()    { p-color   9 "$@" ; }
p-green()  { p-color   2 "$@" ; }
p-blue()   { p-color   4 "$@" ; }
p-yellow() { p-color   3 "$@" ; }
p-purple() { p-color 213 "$@" ; }

p-debug()  { p-purple "DEBUG: $@\n" ; }
p-info()   { p-blue   " INFO: $@\n" ; }
p-warn()   { p-yellow " WARN: $@\n" ; }
p-error()  { p-red    "ERROR: $@\n" ; }
p-success(){ p-green "✅  OK: $@\n" ; }

# ============================================================================
# usage-prompt
#
# Standard help/required-arg handling for subcommands. Call as the first line
# of a run-<name>() function:
#
#   usage-prompt "./run name [args] — description" "$1"
#   usage-prompt "./run name <arg> — description" "$1" "" "required"
#
# Args:
#   $1 help_text  — usage line to print on -h/--help or error
#   $2 help_flag  — typically "$1" of the calling function (the user's first arg)
#   $3 error      — optional error message for -e/--error mode
#   $4 required   — pass "required" to treat empty $help_flag as a usage error
# ============================================================================

usage-prompt() {
    local help_text=$1
    local help_flag=$2
    local error=${3:-"usage error"}
    local required=${4:-""}
    case $help_flag in
        "-h" | "--help")
            echo ""
            printf "$(p-red "Usage:") %s\n" "$help_text"
            exit 0
            ;;
        "-e" | "--error")
            p-error "$error"
            echo ""
            printf "$(p-red "Usage:") %s\n" "$help_text"
            exit 1
            ;;
        "")
            if [ "$required" = "required" ]; then
                echo ""
                printf "$(p-red "Usage:") %s\n" "$help_text"
                exit 1
            fi
            ;;
        *)
            ;;
    esac
}

# ============================================================================
# Interactive helpers
# ============================================================================

ask-yes-no() {
    echo ""
    while true; do
        t-yellow
        read -p "$1 [y/n]: " yn
        case $yn in
            [Yy]* ) return 0;;
            [Nn]* ) return 1;;
            * ) p-warn "Please answer yes or no?";;
        esac
    done
}
