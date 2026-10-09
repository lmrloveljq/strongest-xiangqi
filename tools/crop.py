import sys
from PIL import Image

src, dst = sys.argv[1], sys.argv[2]
box = [int(v) for v in sys.argv[3].split(",")]  # left,top,right,bottom
scale = float(sys.argv[4]) if len(sys.argv) > 4 else 2.0

img = Image.open(src).convert("RGB")
crop = img.crop(tuple(box))
w, h = crop.size
crop = crop.resize((int(w * scale), int(h * scale)), Image.LANCZOS)
crop.save(dst)
print("saved", dst, crop.size)
