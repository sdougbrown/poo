Call a registered CLI:
- "name" is one line of the catalog, and "args" is an object of that CLI's arguments.
- Arguments that do not fit the CLI's schema are refused before anything runs, with the reason.
- The result is the command's output and its exit status, elided in the middle when it is long.
