"""The HTTP server."""


def serve():
    from . import settings
    config = settings.load()
    return config
