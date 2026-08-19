from pathlib import Path

path = Path('.github/scripts/pr16_hardening_runner.py')
text = path.read_text()
old = '''    lines = [line[10:] if line.startswith("          ") else line for line in block.splitlines()]\n'''
new = '''    lines = []\n    in_python_triple = False\n    for line in block.splitlines():\n        # The historical helper was invalid YAML precisely because Python\n        # triple-quoted replacement strings were not indented as YAML block\n        # content. Strip the ten-space YAML prefix from executable Python/shell\n        # lines, but preserve the literal contents of Python triple-quoted\n        # strings byte-for-byte.\n        if in_python_triple:\n            cooked = line\n        else:\n            cooked = line[10:] if line.startswith("          ") else line\n        if line.count("'''") % 2 == 1:\n            in_python_triple = not in_python_triple\n        lines.append(cooked)\n'''
if text.count(old) != 1:
    raise SystemExit('extractor target not found exactly once')
path.write_text(text.replace(old, new, 1))
