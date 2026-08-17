import re
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit

_SENSITIVE_QUERY_KEYS = {
    "token",
    "accesstoken",
    "refreshtoken",
    "idtoken",
    "apikey",
    "key",
    "password",
    "passwd",
    "pwd",
    "secret",
    "clientsecret",
    "auth",
    "authorization",
    "credential",
    "credentials",
    "code",
}


def _is_sensitive_query_key(value: str) -> bool:
    key = re.sub(r"[^a-z0-9]", "", value.lower())
    return key in _SENSITIVE_QUERY_KEYS or key.endswith(
        (
            "token",
            "apikey",
            "password",
            "passwd",
            "secret",
            "auth",
            "credential",
            "code",
        )
    )


def validate_url(value: object) -> str:
    if not isinstance(value, str):
        raise TypeError("URL must be a string")
    parsed = urlsplit(value)
    if parsed.scheme not in {"http", "https"} or not parsed.hostname:
        raise ValueError("URL must use HTTP(S) and include a hostname")
    if parsed.username is not None or parsed.password is not None:
        raise ValueError("URLs containing credentials are not allowed")
    if any(
        _is_sensitive_query_key(key)
        for key, _ in parse_qsl(parsed.query, keep_blank_values=True)
    ):
        raise ValueError("URLs containing sensitive query parameters are not allowed")
    return value


def sanitize_url(value: str) -> str:
    try:
        parsed = urlsplit(value)
        hostname = parsed.hostname
        if not hostname:
            return "[invalid URL]"
        host = f"[{hostname}]" if ":" in hostname else hostname
        port = f":{parsed.port}" if parsed.port is not None else ""
        query = urlencode(
            [
                (key, item)
                for key, item in parse_qsl(parsed.query, keep_blank_values=True)
                if not _is_sensitive_query_key(key)
            ]
        )
        return urlunsplit(
            (parsed.scheme, f"{host}{port}", parsed.path, query, parsed.fragment)
        )
    except ValueError:
        return "[invalid URL]"
