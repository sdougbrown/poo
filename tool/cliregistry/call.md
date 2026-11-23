Call a registered CLI:
- "name" is one line of the catalog, and "args" is an object of that CLI's arguments.
- Pass "background": true to run the call as a background job. The call returns at once with a job handle; the result arrives later as a message.
- Arguments that do not fit the CLI's schema are refused before anything runs, with the reason.
- The result is the command's output and its exit status, elided in the middle when it is long. A tool whose manifest decodes JSON (output mode json) returns that JSON instead of the exit-status envelope when it fits. A JSON null, or decoded JSON that is too long, still goes through the envelope with its output elided.
