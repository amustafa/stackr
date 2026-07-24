# stackr.plugin.zsh — zsh plugin for stackr (`sr`)
#
# Antidote/oh-my-zsh compatible. Install by adding this repo to your plugin
# list, e.g. in ~/.zsh_plugins.txt:
#
#     amustafa/stackr
#
# The plugin only wires up stackr's shell integration when the `sr` binary is
# actually installed and on your $PATH (e.g. after `make link` or
# `make install`). If stackr isn't installed yet, loading the plugin is a
# no-op — no errors, no half-initialised state.

# Guard: bail out quietly unless the `sr` binary is on $PATH.
# `$+commands[sr]` is zsh's built-in PATH lookup — true only if `sr` resolves.
(( $+commands[sr] )) || return 0

# Worktree auto-cd wrapper: defines an `sr()` shell function so navigating to a
# branch in a worktree changes your directory. Equivalent to the documented
# `eval "$(sr shell-hook)"`.
eval "$(command sr shell-hook)"

# Zsh completions. `sr completion zsh` emits a `#compdef` script that calls
# `compdef`, so the completion system must be initialised first. Most zsh
# setups run `compinit` already; initialise it here only if it hasn't been,
# so the plugin works standalone.
if (( ! $+functions[compdef] )); then
  autoload -Uz compinit && compinit
fi
eval "$(command sr completion zsh)"
