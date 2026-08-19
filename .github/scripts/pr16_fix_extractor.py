from pathlib import Path

path = Path('.github/scripts/pr16_hardening_runner.py')
text = path.read_text()
old = '''    lines = [line[10:] if line.startswith("          ") else line for line in block.splitlines()]\n'''
new = """    lines = []
    in_python_triple = False
    for line in block.splitlines():
        # The historical helper was invalid YAML precisely because Python
        # triple-quoted replacement strings were not indented as YAML block
        # content. Strip the ten-space YAML prefix from executable Python/shell
        # lines, but preserve the literal contents of Python triple-quoted
        # strings byte-for-byte.
        if in_python_triple:
            cooked = line
        else:
            cooked = line[10:] if line.startswith("          ") else line
        if line.count("'''") % 2 == 1:
            in_python_triple = not in_python_triple
        lines.append(cooked)
"""
if text.count(old) != 1:
    raise SystemExit('extractor target not found exactly once')
path.write_text(text.replace(old, new, 1))
