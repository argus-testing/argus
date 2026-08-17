class AgentRuntimeError(Exception):
    pass


class UnknownProviderError(AgentRuntimeError):
    pass


class UnknownToolError(AgentRuntimeError):
    pass


class InvalidProviderResponseError(AgentRuntimeError):
    pass


class ModelCallLimitExceeded(AgentRuntimeError):
    pass


class ProviderError(AgentRuntimeError):
    def __init__(
        self,
        message: str,
        *,
        status_code: int | None = None,
        retry_after: float | None = None,
    ) -> None:
        super().__init__(message)
        self.status_code = status_code
        self.retry_after = retry_after


class RateLimitedError(ProviderError):
    pass
