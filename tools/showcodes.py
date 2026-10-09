import sys

path = sys.argv[1]
needle = sys.argv[2]
with open(path, encoding="utf-8") as f:
    for i, line in enumerate(f, 1):
        if needle in line:
            print("LINE", i)
            print("TEXT", line.rstrip())
            print("CODES", " ".join("U+%04X(%s)" % (ord(ch), ch if ch.isprintable() else "?") for ch in line.rstrip()))
