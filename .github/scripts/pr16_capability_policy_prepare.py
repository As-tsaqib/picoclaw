from pathlib import Path

p = Path('.github/scripts/pr16_capability_policy_fix.py')
text = p.read_text()
old = '''    if text.count(old) != 1:
        raise SystemExit(f"{path}: expected exactly one match, found {text.count(old)}")
    p.write_text(text.replace(old, new, 1))
'''
new = '''    expected = 2 if path == "pkg/agent/pipeline_llm.go" and "SenderID:" in old else 1
    if text.count(old) != expected:
        raise SystemExit(f"{path}: expected exactly {expected} match(es), found {text.count(old)}")
    p.write_text(text.replace(old, new, expected))
'''
if old not in text:
    raise SystemExit('replace_once implementation not found')
p.write_text(text.replace(old, new, 1))
