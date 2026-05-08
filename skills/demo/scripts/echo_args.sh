#!/bin/sh

# Safe demo script for MiniAgent skill learning.
# It prints its arguments and does not read, write, or delete workspace files.

printf 'MiniAgent demo skill script\n'
printf 'arguments:'

for arg in "$@"; do
	printf ' [%s]' "$arg"
done

printf '\n'
