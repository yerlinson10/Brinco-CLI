# Bash completion for brinco (basic). Optional: requires bash 4+ for some setups.
# Add to ~/.bashrc:  source /path/to/brinco-cli/completions/brinco.bash

_brinco() {
	local cur
	COMPREPLY=()
	cur=${COMP_WORDS[COMP_CWORD]}

	local cmds='help version doctor host join relay update room'
	local relay_flags='--listen --public --max-per-ip --max-connections'
	local room_flags='--name --mode --relay --password --pass --code --direct --listen --public --notify-sound --notify-level --file-limit'

	case ${COMP_WORDS[1]} in
	room)
		if [[ $COMP_CWORD -eq 2 ]]; then
			COMPREPLY=($(compgen -W 'create join code help' -- "$cur"))
			return
		fi
		if [[ ${COMP_WORDS[2]} == create || ${COMP_WORDS[2]} == join ]]; then
			COMPREPLY=($(compgen -W "$room_flags" -- "$cur"))
		fi
		;;
	relay)
		COMPREPLY=($(compgen -W "serve help $relay_flags" -- "$cur"))
		;;
	update)
		COMPREPLY=($(compgen -W 'check apply help' -- "$cur"))
		;;
	*)
		if [[ $COMP_CWORD -eq 1 ]]; then
			COMPREPLY=($(compgen -W "$cmds" -- "$cur"))
		fi
		;;
	esac
}

complete -F _brinco brinco
