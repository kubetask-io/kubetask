#!/bin/bash
# Stream JSON Reader for opencode CLI output
#
# Converts stream-json format to human-readable output.
# Usage: opencode run ... --format json | opencode-stream-json-reader
#
# Key principle: Never swallow unknown output - if a line cannot be parsed,
# print it as-is to ensure no information is lost.

set -euo pipefail

# Colors (ANSI escape codes)
RESET='\033[0m'
BOLD='\033[1m'
DIM='\033[2m'
CYAN='\033[36m'
GREEN='\033[32m'
YELLOW='\033[33m'
RED='\033[31m'
BLUE='\033[34m'
MAGENTA='\033[35m'

# Box drawing characters
BOX_TL='╭'
BOX_TR='╮'
BOX_BL='╰'
BOX_BR='╯'
BOX_H='─'
BOX_V='│'

# Track state for streaming messages
in_text_stream=false
current_session=""
step_count=0

# Print a separator line
print_separator() {
    echo -e "${DIM}────────────────────────────────────────${RESET}"
}

# Print session header
print_session_header() {
    local session_id="$1"
    local width=64
    local line=$(printf '%*s' $((width - 2)) '' | tr ' ' "$BOX_H")

    echo -e "${CYAN}${BOX_TL}${line}${BOX_TR}${RESET}"
    printf "${CYAN}${BOX_V}${RESET} %-$((width - 4))s ${CYAN}${BOX_V}${RESET}\n" "Session: ${session_id:0:40}"
    echo -e "${CYAN}${BOX_BL}${line}${BOX_BR}${RESET}"
    echo
}

# Format step_start
format_step_start() {
    local json="$1"
    local session_id

    session_id=$(echo "$json" | jq -r '.sessionID // "unknown"')

    # Print session header on first step
    if [[ "$current_session" != "$session_id" ]]; then
        current_session="$session_id"
        step_count=0
        print_session_header "$session_id"
    fi

    ((++step_count))
    echo -e "${DIM}[Step $step_count]${RESET}"
}

# Format text message
format_text() {
    local json="$1"
    local text

    text=$(echo "$json" | jq -r '.part.text // ""')

    if [[ -z "$text" ]]; then
        return
    fi

    if [[ "$in_text_stream" == "false" ]]; then
        echo -e "\n${GREEN}${BOLD}[ASSISTANT]${RESET}"
        in_text_stream=true
    fi

    echo "$text"
}

# Format tool_use
format_tool_use() {
    local json="$1"
    local tool_name call_id status input output title

    # End any ongoing text stream
    if [[ "$in_text_stream" == "true" ]]; then
        echo
        in_text_stream=false
    fi

    tool_name=$(echo "$json" | jq -r '.part.tool // "unknown"')
    call_id=$(echo "$json" | jq -r '.part.callID // ""')
    status=$(echo "$json" | jq -r '.part.state.status // "unknown"')
    title=$(echo "$json" | jq -r '.part.state.title // empty')
    input=$(echo "$json" | jq -r '.part.state.input // {}')
    output=$(echo "$json" | jq -r '.part.state.output // ""')

    echo -e "\n${YELLOW}${BOLD}[TOOL] ${tool_name}${RESET}"

    # Show title if present
    if [[ -n "$title" ]]; then
        echo -e "   ${DIM}Title:${RESET} $title"
    fi

    # Show key input parameters
    local path command description
    path=$(echo "$input" | jq -r '.path // empty' 2>/dev/null || true)
    command=$(echo "$input" | jq -r '.command // empty' 2>/dev/null || true)
    description=$(echo "$input" | jq -r '.description // empty' 2>/dev/null || true)

    if [[ -n "$description" ]]; then
        echo -e "   ${DIM}Description:${RESET} $description"
    fi

    if [[ -n "$path" ]]; then
        echo -e "   ${DIM}Path:${RESET} ${CYAN}$path${RESET}"
    fi

    if [[ -n "$command" ]]; then
        echo -e "   ${DIM}Command:${RESET} ${CYAN}$command${RESET}"
    fi

    # Show status
    if [[ "$status" == "completed" ]]; then
        echo -e "   ${GREEN}[OK]${RESET}"
    elif [[ "$status" == "pending" ]]; then
        echo -e "   ${YELLOW}[PENDING]${RESET}"
    else
        echo -e "   ${RED}[${status}]${RESET}"
    fi

    # Print output with indentation, truncate if too long
    if [[ -n "$output" && "$output" != "null" ]]; then
        local line_count
        line_count=$(echo "$output" | wc -l)

        if [[ $line_count -gt 20 ]]; then
            echo "$output" | head -15 | sed 's/^/   /'
            echo -e "   ${DIM}... ($((line_count - 15)) more lines)${RESET}"
        else
            echo "$output" | sed 's/^/   /'
        fi
    fi
}

# Format step_finish
format_step_finish() {
    local json="$1"
    local reason cost input_tokens output_tokens reasoning_tokens

    # End any ongoing text stream
    if [[ "$in_text_stream" == "true" ]]; then
        echo
        in_text_stream=false
    fi

    reason=$(echo "$json" | jq -r '.part.reason // "unknown"')
    cost=$(echo "$json" | jq -r '.part.cost // 0')
    input_tokens=$(echo "$json" | jq -r '.part.tokens.input // 0')
    output_tokens=$(echo "$json" | jq -r '.part.tokens.output // 0')
    reasoning_tokens=$(echo "$json" | jq -r '.part.tokens.reasoning // 0')

    echo
    print_separator
    echo -e "${DIM}Reason: $reason | Cost: \$${cost} | Tokens: in=$input_tokens out=$output_tokens reason=$reasoning_tokens${RESET}"
    print_separator
    echo
}

# Main processing loop
while IFS= read -r line || [[ -n "$line" ]]; do
    # Skip empty lines
    if [[ -z "$line" ]]; then
        continue
    fi

    # Try to parse as JSON and get type
    msg_type=$(echo "$line" | jq -r '.type // empty' 2>/dev/null) || msg_type=""

    if [[ -z "$msg_type" ]]; then
        # Not valid JSON or no type field - print as-is
        echo "$line"
        continue
    fi

    case "$msg_type" in
        "step_start")
            format_step_start "$line"
            ;;
        "text")
            format_text "$line"
            ;;
        "tool_use")
            format_tool_use "$line"
            ;;
        "step_finish")
            format_step_finish "$line"
            ;;
        *)
            # Unknown type - print raw line
            echo "$line"
            ;;
    esac
done

# End any ongoing text stream
if [[ "$in_text_stream" == "true" ]]; then
    echo
fi
