import os
import sys
from pathlib import Path

import psycopg


def load_dotenv(dotenv_path: Path) -> None:
    if not dotenv_path.is_file():
        return
    for line in dotenv_path.read_text(encoding="utf-8").splitlines():
        raw = line.strip()
        if not raw or raw.startswith("#") or "=" not in raw:
            continue
        key, value = raw.split("=", 1)
        key = key.strip()
        value = value.strip().strip("'").strip('"')
        if key and key not in os.environ:
            os.environ[key] = value


def main() -> int:
    dotenv_path = Path(__file__).resolve().parents[1] / ".env"
    load_dotenv(dotenv_path)

    database_url = os.getenv("DATABASE_URL")
    if not database_url:
        print("DATABASE_URL is not set.")
        return 1

    migrations_dir = Path(__file__).resolve().parents[1] / "migrations"
    if not migrations_dir.is_dir():
        print(f"migrations directory not found: {migrations_dir}")
        return 1

    files = sorted(migrations_dir.glob("*.sql"))
    if not files:
        print("No migration files found.")
        return 1

    try:
        with psycopg.connect(database_url) as conn:
            with conn.cursor() as cur:
                for path in files:
                    sql = path.read_text(encoding="utf-8")
                    print(f"Running {path.name}...")
                    cur.execute(sql)
            conn.commit()
    except Exception as exc:
        print(f"Migration failed: {exc}")
        return 1

    print("Migrations applied.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
