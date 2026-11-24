Call a registered command:
- call_cli is only for the commands the catalog lists. It is not a dispatcher: bash, read, grep, history, edit, fetch, codemode, jobs, finish and the rest are tools you call directly, and call_cli cannot run them, itself, or anything the catalog does not list.
- "command" is one line of the catalog, and "args" is an object of that command's arguments.
- Pass "background": true to run the call asynchronously as a background job. The call returns at once with a job handle; the result arrives later as a message.
- Arguments that do not fit the command's schema are refused before anything runs, with the reason.
- The result is the command's output and its exit status, elided in the middle when it is long. A command whose manifest decodes JSON (output mode json) returns that JSON instead of the exit-status envelope when it fits. A JSON null, or decoded JSON that is too long, still goes through the envelope with its output elided.
