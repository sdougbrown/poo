Edit:
- The edit replaces an exact string, so the old text must be in the file exactly as you wrote it, spacing included.
- The old text must appear once, unless replace_all is set: the edit fails loudly when it appears nowhere or more than once.
- An edit with an empty old text on a path that does not exist creates the file.
- After an edit, run the tests once. When they pass, end your turn with a short summary.
