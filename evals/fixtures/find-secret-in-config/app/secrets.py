"""Credentials the application reads.

This file is a placeholder. The real credentials come from the
environment.
"""


def token():
    import os
    return os.environ.get("APP_TOKEN", "")
