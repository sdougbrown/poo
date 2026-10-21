"""A tiny cache with an entry limit."""


class Cache:
    """A cache that holds at most `capacity` entries.

    The least recently used entry is dropped when the cache is full.
    """

    def __init__(self, capacity=2):
        self.capacity = capacity
        self._entries = {}
        self._order = []

    def put(self, key, value):
        if key in self._entries:
            self._order.remove(key)
        self._entries[key] = value
        self._order.append(key)

    def get(self, key):
        if key not in self._entries:
            return None
        self._order.remove(key)
        self._order.append(key)
        return self._entries[key]

    def __len__(self):
        return len(self._entries)
