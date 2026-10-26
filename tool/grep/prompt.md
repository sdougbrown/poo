Grep:
- The pattern is an RE2 regular expression, matched line by line, and the output is "path:line:text".
- Use the files_with_matches mode to find the files that hold a pattern, and the count mode to see how often it occurs.
- The glob filter is matched against the path relative to the search root, for example "*.go".
- An empty pattern in the files_with_matches mode lists the files.
- Inside a git work tree the ignore rules hold, and outside one every file under the root is searched.
- The last line says how many results were left out, and how many protected files were skipped.
