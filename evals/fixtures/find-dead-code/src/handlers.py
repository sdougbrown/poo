"""Request handlers."""


def handle_root(request):
    return {"status": 200, "body": "ok"}


def handle_health(request):
    return {"status": 200, "body": "up"}


def handle_legacy_ping(request):
    return {"status": 200, "body": "pong"}


def handle_metrics(request):
    return {"status": 200, "body": "metrics"}
