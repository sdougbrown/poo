History:
- The history tool reads the turns this session's store retains, including the turns a compaction replaced; the store keeps what the view no longer shows.
- A call reads one turn whole (`turn`), or searches the retained turns for an RE2 regular expression (`pattern`); a call does one of the two.
- A search's output is "turn N: line", and the last line says how many results were left out.
- A turn read holds the full text, so it is the way back to a detail a search names.
- The turn numbers are the session's own: the number a compaction marker names is the number the tool reads.
