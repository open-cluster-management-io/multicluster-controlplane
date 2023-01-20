#!/usr/bin/env bash

# Controls verbosity of the script output and logging.
KUBE_VERBOSE="${KUBE_VERBOSE:-2}"

# Handler for when we exit automatically on an error.
# Borrowed from https://gist.github.com/ahendrix/7030300
kube::log::errexit() {
  local err="${PIPESTATUS[*]}"

  # If the shell we are in doesn't have errexit set (common in subshells) then
  # don't dump stacks.
  set +o | grep -qe "-o errexit" || return

  set +o xtrace
  local code="${1:-1}"
  # Print out the stack trace described by $function_stack
  if [ ${#FUNCNAME[@]} -gt 2 ]
  then
    kube::log::error "Call tree:"
    for ((i=1;i<${#FUNCNAME[@]}-1;i++))
    do
      kube::log::error " ${i}: ${BASH_SOURCE[${i}+1]}:${BASH_LINENO[${i}]} ${FUNCNAME[${i}]}(...)"
    done
  fi
  kube::log::error_exit "Error in ${BASH_SOURCE[1]}:${BASH_LINENO[0]}. '${BASH_COMMAND}' exited with status ${err}" "${1:-1}" 1
}

kube::log::install_errexit() {
  # trap ERR to provide an error handler whenever a command exits nonzero  this
  # is a more verbose version of set -o errexit
  trap 'kube::log::errexit' ERR

  # setting errtrace allows our ERR trap handler to be propagated to functions,
  # expansions and subshells
  set -o errtrace
}

# Log an error but keep going.  Don't dump the stack or exit.
kube::log::error() {
  timestamp=$(date +"[%m%d %H:%M:%S]")
  echo "!!! ${timestamp} ${1-}" >&2
  shift
  for message; do
    echo "    ${message}" >&2
  done
}

# Print a status line.  Formatted to show up in a stream of output.
kube::log::status() {
  local V="${V:-0}"
  if [[ ${KUBE_VERBOSE} < ${V} ]]; then
    return
  fi

  timestamp=$(date +"[%m%d %H:%M:%S]")
  echo "+++ ${timestamp} ${1}"
  shift
  for message; do
    echo "    ${message}"
  done
}

# Print an usage message to stderr.  The arguments are printed directly.
kube::log::usage() {
  echo >&2
  local message
  for message; do
    echo "${message}" >&2
  done
  echo >&2
}

# Print out some info that isn't a top level status line
kube::log::info() {
  local V="${V:-0}"
  if [[ ${KUBE_VERBOSE} < ${V} ]]; then
    return
  fi

  for message; do
    echo "${message}"
  done
}

# Log an error and exit.
# Args:
#   $1 Message to log with the error
#   $2 The error code to return
#   $3 The number of stack frames to skip when printing.
kube::log::error_exit() {
  local message="${1:-}"
  local code="${2:-1}"
  local stack_skip="${3:-0}"
  stack_skip=$((stack_skip + 1))

  if [[ ${KUBE_VERBOSE} -ge 4 ]]; then
    local source_file=${BASH_SOURCE[${stack_skip}]}
    local source_line=${BASH_LINENO[$((stack_skip - 1))]}
    echo "!!! Error in ${source_file}:${source_line}" >&2
    [[ -z ${1-} ]] || {
      echo "  ${1}" >&2
    }

    kube::log::stack ${stack_skip}

    echo "Exiting with status ${code}" >&2
  fi

  exit "${code}"
}
