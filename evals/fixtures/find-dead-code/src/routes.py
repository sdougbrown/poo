"""The routing table."""

from . import handlers

ROUTES = {
    "/": handlers.handle_root,
    "/health": handlers.handle_health,
    "/metrics": handlers.handle_metrics,
}
