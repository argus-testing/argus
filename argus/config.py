import os
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True, slots=True)
class Settings:
    data_dir: Path = Path("data")
    gemini_api_key: str | None = None
    gemini_model: str = "gemini-2.5-flash"
    headless: bool = True
    run_timeout_seconds: int = 300

    @classmethod
    def from_env(cls) -> "Settings":
        return cls(
            data_dir=Path(os.getenv("ARGUS_DATA_DIR", "data")),
            gemini_api_key=os.getenv("GEMINI_API_KEY"),
            gemini_model=os.getenv("GEMINI_MODEL", "gemini-2.5-flash"),
            headless=os.getenv("ARGUS_HEADLESS", "true").lower()
            not in {"0", "false", "no"},
            run_timeout_seconds=int(os.getenv("ARGUS_RUN_TIMEOUT", "300")),
        )
