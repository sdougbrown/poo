Read:
- The read shows a range of lines and names it, for example "[path · lines 1–2000 of 5312]".
- Carry on from the last line you saw with offset, instead of reading the file again from the start.
- A read that says "capped" stopped at the context budget: ask for the next range with offset.
- A range past the end of the file comes back empty, with the file's line count in the header.
- A binary file is refused, with its size; use grep or a command to look inside it.
