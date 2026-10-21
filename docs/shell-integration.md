# Shell integration (command marks)

When a shell reports its prompts and commands with OSC 133 escape sequences, Keel marks each finished command in the terminal's left inset (a green dot for exit 0, a red one otherwise, with an `exit N` chip at the end of a failed command's prompt line), the palette's **Previous command** / **Next command** scroll between prompts, and the agent reads the last command, its output and its exit status instead of the pane's last lines (`docs/ui-redesign.md` §14).

Keel never edits a remote host's shell files, and does not change how the Local Shell starts. fish 4.0 and later already send the marks; for bash and zsh, and fish 3, add one of these snippets yourself, on each host where you want it (the Local Shell reads your local files). It only prints escape sequences around your prompt; it does not change what commands do.

## bash

bash 4.4 or later, at the end of `~/.bashrc` (macOS's `/bin/bash` 3.2 has no `PS0`, so it gets prompts but no commands):

```bash
# Keel shell integration (OSC 133)
if [[ $- == *i* && -z ${__keel_osc133-} ]]; then
  __keel_osc133=1
  __keel_prompt() {
    local s=$?
    printf '\033]133;D;%s\007' "$s"
    return "$s"
  }
  PROMPT_COMMAND="__keel_prompt${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
  PS1='\[\033]133;A\007\]'"$PS1"'\[\033]133;B\007\]'
  PS0='\033]133;C\007'
fi
```

## zsh

At the end of `~/.zshrc`:

```zsh
# Keel shell integration (OSC 133)
if [[ -o interactive && -z ${__keel_osc133-} ]]; then
  __keel_osc133=1
  __keel_precmd() {
    local s=$?
    (( ${+__keel_ran} )) && printf '\033]133;D;%s\007' "$s"
    unset __keel_ran
    printf '\033]133;A\007'
  }
  __keel_preexec() { __keel_ran=1; printf '\033]133;C\007'; }
  precmd_functions=(__keel_precmd $precmd_functions)
  preexec_functions+=(__keel_preexec)
  PS1="$PS1%{$(printf '\033]133;B\007')%}"
fi
```

## fish

fish 4 needs nothing. fish 3, in `~/.config/fish/config.fish`:

```fish
# Keel shell integration (OSC 133)
if status is-interactive; and test (string split -f1 . $version) -lt 4; and not set -q __keel_osc133
    set -g __keel_osc133 1
    function __keel_prompt --on-event fish_prompt
        printf '\e]133;A\a'
    end
    function __keel_preexec --on-event fish_preexec
        printf '\e]133;C\a'
    end
    function __keel_postexec --on-event fish_postexec
        printf '\e]133;D;%s\a' $status
    end
end
```

## Prompt themes

Prompt themes that rebuild `PS1` on every prompt (powerlevel10k, starship, bash-git-prompt) drop the `B` mark the snippet adds; marks still work, but the agent then sees the command's output without the command line. powerlevel10k can send the marks itself (`POWERLEVEL9K_TERM_SHELL_INTEGRATION=true`).

The marks are output like any other, so a program can print fake ones: they only draw marks and choose what the agent reads, they never run or allow anything.
