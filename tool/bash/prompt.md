Bash:
- Every bash call runs in a fresh shell. A cd, an exported variable or a background job from one call is gone by the next one. Use absolute paths, or start each command where you want it.
- A command runs where the call starts it: `cd build && make` changes the directory for that call only, and `ls` on its own lists the working directory.
- Background processes are killed when the command returns.
- Work that has to keep running belongs in a background call. Pass "background": true and the call returns at once; the result arrives later as a message.
- Long output is truncated with the middle elided. Use grep/sed/head/tail to inspect narrower ranges.
