# Python standalone sample

This directory contains simple sample scripts to test Whatap Python Agent in standalone mode.

If you moved your working project to /Users/jaeyoung/work/python-standalone and want these samples there, run the export helper from the repo root:

```bash
./scripts/export-python-standalone.sh /Users/jaeyoung/work/python-standalone
```

It will copy:
- app_single.py
- app_multi.py

and generate a minimal README.md at the target, if not present.

## Quick run (here)
```bash
python3 app_single.py
python3 app_multi.py
```

## With Whatap standalone agent
Single-transaction:
```bash
STANDALONE_ENABLED=true whatap-start-agent app_single.py
```

Multiple-transaction:
```bash
STANDALONE_ENABLED=true \
STANDALONE_TYPE=multiple-transaction \
STANDALONE_TRANSACTION_PATTERNS="__main__:task_alpha,__main__:Worker.run" \
whatap-start-agent app_multi.py
```
