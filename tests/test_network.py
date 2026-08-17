# pyright: reportMissingImports=false
import pytest

from argus.network import sanitize_url, validate_url


@pytest.mark.parametrize(
    "url",
    [
        "https://user:pass@example.com",
        "https://example.com/?token=x",
        "https://example.com/?api_key=x",
        "https://example.com/?x-api-key=x",
        "https://example.com/?auth_token=x",
        "https://example.com/?sessionToken=x",
        "https://example.com/?password=x",
        "https://example.com/?client_secret=x",
        "https://example.com/?auth=x",
        "https://example.com/?credential=x",
        "https://example.com/?code=x",
    ],
)
def test_url_rejects_credentials_and_sensitive_queries(url):
    with pytest.raises(ValueError):
        validate_url(url)


@pytest.mark.parametrize(
    "url",
    [
        "http://localhost:3000",
        "http://127.0.0.1:8000",
        "http://[::1]:5173",
        "http://10.0.0.2",
        "https://example.com/path?view=list",
    ],
)
def test_url_allows_normal_http_targets_including_local_apps(url):
    assert validate_url(url) == url


def test_sanitize_url_removes_userinfo_and_sensitive_queries():
    assert (
        sanitize_url(
            "https://user:pass@example.com/path?view=list&access_token=secret#section"
        )
        == "https://example.com/path?view=list#section"
    )
