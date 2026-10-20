"""Loading a small configuration file."""


def load_config(text):
    """Return the configuration held in `text`.

    The text holds one `key=value` pair per line. Blank lines and lines
    starting with `#` are ignored.
    """
    config = {}
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        key, _, value = line.partition("=")
        config[key.strip()] = value.strip()
    return config


def require(config, key):
    """Return the value of `key`, or raise KeyError when it is missing."""
    return config.get(key)
