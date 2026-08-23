"""Minimal reader for the MySQL dumps that ship 4CLOP's game content.

The dumps use standard SQL string quoting: single quotes, with an embedded
quote written as two quotes in a row. There are no backslash escapes (verified
against `clop/tables with data.sql`), so a small hand-rolled scanner is enough
and we avoid needing a running MySQL just to read static content.
"""

from __future__ import annotations

import re
from typing import Any, Dict, List, Optional, Sequence

_INSERT_RE = re.compile(
    r"INSERT INTO\s+`(?P<table>\w+)`\s*\((?P<cols>[^)]*)\)\s*VALUES\s*",
    re.IGNORECASE,
)
_COL_RE = re.compile(r"`(\w+)`")

Row = Dict[str, Any]


class SqlDumpError(RuntimeError):
    """The dump did not look the way we expect."""


def _scan_tuples(text: str, start: int) -> tuple[List[List[str]], int]:
    """Read the `(...),(...);` value list beginning at `start`.

    Returns the raw field slices per tuple, plus the index just past the
    terminating semicolon. Splitting has to be quote-aware because the
    descriptions contain both commas and parentheses.
    """
    tuples: List[List[str]] = []
    i = start
    n = len(text)

    while i < n:
        while i < n and text[i] in " \t\r\n":
            i += 1
        if i >= n or text[i] == ";":
            return tuples, i + 1
        if text[i] != "(":
            raise SqlDumpError(f"expected '(' at offset {i}, found {text[i]!r}")

        i += 1
        fields: List[str] = []
        field_start = i
        in_string = False

        while i < n:
            ch = text[i]
            if in_string:
                if ch == "'":
                    # Two quotes in a row is an escaped quote, not the end.
                    if i + 1 < n and text[i + 1] == "'":
                        i += 2
                        continue
                    in_string = False
                i += 1
                continue
            if ch == "'":
                in_string = True
                i += 1
                continue
            if ch == ",":
                fields.append(text[field_start:i])
                i += 1
                field_start = i
                continue
            if ch == ")":
                fields.append(text[field_start:i])
                i += 1
                break
            i += 1
        else:
            raise SqlDumpError("unterminated value tuple")

        tuples.append(fields)

        while i < n and text[i] in " \t\r\n":
            i += 1
        if i < n and text[i] == ",":
            i += 1

    raise SqlDumpError("value list ran off the end of the file")


def _coerce(raw: str) -> Any:
    """Turn one raw SQL field into a Python value."""
    field = raw.strip()
    if not field:
        return None
    if field.upper() == "NULL":
        return None
    if field.startswith("'"):
        if not field.endswith("'") or len(field) < 2:
            raise SqlDumpError(f"malformed string literal: {field[:40]!r}")
        return field[1:-1].replace("''", "'")
    if re.fullmatch(r"-?\d+", field):
        return int(field)
    if re.fullmatch(r"-?\d*\.\d+", field):
        return float(field)
    raise SqlDumpError(f"unrecognised literal: {field[:40]!r}")


def parse_dump(text: str) -> Dict[str, List[Row]]:
    """Return {table_name: [row dicts]} for every INSERT in the dump."""
    tables: Dict[str, List[Row]] = {}

    for match in _INSERT_RE.finditer(text):
        table = match.group("table")
        columns = _COL_RE.findall(match.group("cols"))
        if not columns:
            raise SqlDumpError(f"no columns parsed for `{table}`")

        raw_tuples, _ = _scan_tuples(text, match.end())
        rows = tables.setdefault(table, [])
        for fields in raw_tuples:
            if len(fields) != len(columns):
                raise SqlDumpError(
                    f"`{table}`: expected {len(columns)} fields, got {len(fields)}"
                )
            rows.append({col: _coerce(val) for col, val in zip(columns, fields)})

    return tables


def index_by(rows: Sequence[Row], key: str) -> Dict[Any, Row]:
    return {row[key]: row for row in rows}


def group_by(rows: Sequence[Row], key: str) -> Dict[Any, List[Row]]:
    grouped: Dict[Any, List[Row]] = {}
    for row in rows:
        grouped.setdefault(row[key], []).append(row)
    return grouped
