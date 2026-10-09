# boardmap.py —— 在真实窗口截图上定位棋盘几何，并把像素坐标反算成 (file,rank)/中文坐标。
# 用法: python boardmap.py <png> [greenColorHex] [amberColorHex]
import sys
from PIL import Image

PLATE = (0xF0, 0xE0, 0xBC)
# canvas.Circle/Rectangle 带 alpha 合成到底板上之后的实际显示色
DOT_ON_PLATE = (82, 143, 99)      # TargetDot 0x2E7D4F @ A=0xD0 over plate
SEL_ON_PLATE = (243, 210, 114)    # SelectFill 0xF4CE5A @ A=0xC0 over plate
LF_ON_PLATE = (150, 179, 211)     # LastFrom 0x6D9ED8 @ A=0x90 over plate


def near(p, c, tol):
    return abs(p[0]-c[0]) <= tol and abs(p[1]-c[1]) <= tol and abs(p[2]-c[2]) <= tol


def plate_bbox(img, tol=6):
    w, h = img.size
    px = img.load()
    minx, miny, maxx, maxy = 10**9, 10**9, -1, -1
    for y in range(0, h, 1):
        for x in range(0, w // 2, 1):
            if near(px[x, y], PLATE, tol):
                if x < minx: minx = x
                if y < miny: miny = y
                if x > maxx: maxx = x
                if y > maxy: maxy = y
    return minx, miny, maxx, maxy


def blobs(img, color, tol, region, minpx):
    x0, y0, x1, y1 = region
    px = img.load()
    seen = set()
    out = []
    for y in range(y0, y1):
        for x in range(x0, x1):
            if (x, y) in seen or not near(px[x, y], color, tol):
                continue
            stack = [(x, y)]; seen.add((x, y))
            mnx = mxx = x; mny = mxy = y; cnt = 0; sx = sy = 0
            while stack:
                cx, cy = stack.pop()
                cnt += 1; sx += cx; sy += cy
                mnx = min(mnx, cx); mxx = max(mxx, cx)
                mny = min(mny, cy); mxy = max(mxy, cy)
                for nx, ny in ((cx+1, cy), (cx-1, cy), (cx, cy+1), (cx, cy-1)):
                    if x0 <= nx < x1 and y0 <= ny < y1 and (nx, ny) not in seen and near(px[nx, ny], color, tol):
                        seen.add((nx, ny)); stack.append((nx, ny))
            if cnt >= minpx:
                out.append(dict(n=cnt, bbox=(mnx, mny, mxx, mxy), c=(sx/cnt, sy/cnt),
                                w=mxx-mnx+1, h=mxy-mny+1))
    out.sort(key=lambda d: -d['n'])
    return out


def main():
    path = sys.argv[1]
    img = Image.open(path).convert('RGB')
    W, H = img.size
    bx0, by0, bx1, by1 = plate_bbox(img)
    pw, ph = bx1-bx0+1, by1-by0+1
    print(f"image {W}x{H}")
    print(f"plate bbox = ({bx0},{by0})-({bx1},{by1})  w={pw} h={ph}")

    # 底板 = 从 x(0)-pad 到 x(8)+pad，从 y(9)-pad 到 y(0)+pad
    # 宽 = (8+2*0.28)*cell, 高 = (9+2*0.28)*cell
    cellx = pw / (8.0 + 0.56)
    celly = ph / (9.0 + 0.56)
    print(f"cell from plate: x={cellx:.3f} y={celly:.3f}")
    cell = (cellx + celly) / 2
    ox = bx0 + 0.28*cell           # x(0) 屏幕像素
    oy_top = by0 + 0.28*cell       # y(9) 屏幕像素（最上面一行）
    print(f"origin x(0)={ox:.2f}  y(rank9)={oy_top:.2f}  cell={cell:.3f}")

    def to_sq(px_, py_):
        f = round((px_ - ox) / cell)
        r = round((py_ - oy_top) / cell)
        return f, 9 - r

    for name, col, tol, minpx in (("target-dot(绿)", DOT_ON_PLATE, 26, 25),
                                  ("selection(琥珀)", SEL_ON_PLATE, 26, 150),
                                  ("lastmove(蓝)", LF_ON_PLATE, 26, 120)):
        res = blobs(img, col, tol, (0, 0, W//2, H), minpx)
        print(f"\n{name}: {len(res)} 个")
        for d in res[:10]:
            f, r = to_sq(d['c'][0], d['c'][1])
            letters = "abcdefghi"
            coord = f"{letters[f]}{r}" if 0 <= f <= 8 and 0 <= r <= 9 else "out"
            print(f"  px={d['n']:6d} center=({d['c'][0]:7.1f},{d['c'][1]:7.1f}) "
                  f"{d['w']}x{d['h']} -> file={f} rank={r}  ({coord})")


main()
