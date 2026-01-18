#!/usr/bin/env python3
import argparse
import sqlite3
import time
import sys

DEFAULT_DB = "/var/lib/kc-policy/aliases.db"

SCHEMA = """
CREATE TABLE IF NOT EXISTS aliases (
  alias_email TEXT PRIMARY KEY,
  username    TEXT NOT NULL,
  enabled     INTEGER NOT NULL DEFAULT 1,
  updated_at  INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);
CREATE INDEX IF NOT EXISTS idx_aliases_username ON aliases(username);
"""

def connect(db_path: str):
    con = sqlite3.connect(db_path)
    con.execute("PRAGMA journal_mode=WAL;")
    con.execute("PRAGMA synchronous=NORMAL;")
    con.executescript(SCHEMA)
    return con

def norm_email(s: str) -> str:
    return s.strip().lower()

def cmd_add(con, alias_email, username):
    alias_email = norm_email(alias_email)
    username = username.strip()
    now = int(time.time())
    con.execute(
        "INSERT INTO aliases(alias_email, username, enabled, updated_at) VALUES(?,?,1,?) "
        "ON CONFLICT(alias_email) DO UPDATE SET username=excluded.username, enabled=1, updated_at=excluded.updated_at",
        (alias_email, username, now),
    )
    con.commit()

def cmd_del(con, alias_email):
    alias_email = norm_email(alias_email)
    con.execute("DELETE FROM aliases WHERE alias_email=?", (alias_email,))
    con.commit()

def cmd_disable(con, alias_email):
    alias_email = norm_email(alias_email)
    con.execute("UPDATE aliases SET enabled=0, updated_at=? WHERE alias_email=?", (int(time.time()), alias_email))
    con.commit()

def cmd_list(con, username=None):
    if username:
        rows = con.execute(
            "SELECT alias_email, username, enabled, updated_at FROM aliases WHERE username=? ORDER BY alias_email",
            (username,),
        ).fetchall()
    else:
        rows = con.execute(
            "SELECT alias_email, username, enabled, updated_at FROM aliases ORDER BY username, alias_email"
        ).fetchall()
    for a,u,en,ts in rows:
        print(f"{a}\t{u}\t{'enabled' if en else 'disabled'}\t{ts}")

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--db", default=DEFAULT_DB)
    sub = ap.add_subparsers(dest="cmd", required=True)

    p_add = sub.add_parser("add")
    p_add.add_argument("alias_email")
    p_add.add_argument("username")

    p_del = sub.add_parser("del")
    p_del.add_argument("alias_email")

    p_dis = sub.add_parser("disable")
    p_dis.add_argument("alias_email")

    p_ls = sub.add_parser("list")
    p_ls.add_argument("--user", default=None)

    args = ap.parse_args()
    con = connect(args.db)
    try:
        if args.cmd == "add":
            cmd_add(con, args.alias_email, args.username)
        elif args.cmd == "del":
            cmd_del(con, args.alias_email)
        elif args.cmd == "disable":
            cmd_disable(con, args.alias_email)
        elif args.cmd == "list":
            cmd_list(con, args.user)
    finally:
        con.close()

if __name__ == "__main__":
    sys.exit(main())
