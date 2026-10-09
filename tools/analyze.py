# analyze.py —— 从真实窗口截图里量出棋盘底板/格线的像素位置，与布局日志对照。
# 用法: python analyze.py <png> [--left-half] [--dots]
import sys
from PIL import Image

PLATE = (0xF0, 0xE0, 0xBC)
DOTS = (0x2E, 0x7D, 0x4F)
SEL = (0xF4, 0xCE, 0x5A)


def near(p, c, tol=6):
    return abs(p[0] - c[0]) <= tol and abs(p[1] - c[1]) <= tol and abs(p[2] - c[2]) <= tol


def bbox_of(img, color, tol=6, x0=0, y0=0, x1=None, y1=None, step=1):
    w, h = img.size
    x1 = w if x1 is None else x1
    y1 = h if y1 is None else y1
    minx, miny, maxx, maxy, n = 10**9, 10**9, -1, -1, 0
    px = img.load()
    for y in range(y0, y1, step):
        for x in range(x0, x1, step):
            if near(px[x, y], color, tol):
                n += 1
                if x < minx: minx = x
                if y < miny: miny = y
                if x > maxx: maxx = x
                if y > maxy: maxy = y
    if n == 0:
        return None
    return minx, miny, maxx, maxy, n


def blobs(img, color, tol=10, x0=0, y0=0, x1=None, y1=None, minpx=40):
    """连通域（4 邻域）简单实现，返回每个簇的包围盒与质心。"""
    w, h = img.size
    x1 = w if x1 is None else x1
    y1 = h if y1 is None else y1
    px = img.load()
    seen = set()
    out = []
    for y in range(y0, y1):
        for x in range(x0, x1):
            if (x, y) in seen or not near(px[x, y], color, tol):
                continue
            stack = [(x, y)]
            seen.add((x, y))
            minx = maxx = x
            miny = maxy = y
            cnt = 0
            sx = sy = 0
            while stack:
                cx, cy = stack.pop()
                cnt += 1
                sx += cx; sy += cy
                if cx < minx: minx = cx
                if cx > maxx: maxx = cx
                if cy < miny: miny = cy
                if cy > maxy: maxy = cy
                for nx, ny in ((cx+1, cy), (cx-1, cy), (cx, cy+1), (cx, cy-1)):
                    if x0 <= nx < x1 and y0 <= ny < y1 and (nx, ny) not in seen and near(px[nx, ny], color, tol):
                        seen.add((nx, ny))
                        stack.append((nx, ny))
            if cnt >= minpx:
                out.append((cnt, minx, miny, maxx, maxy, sx/cnt, sy/cnt))
    out.sort(reverse=True)
    return out


def main():
    path = sys.argv[1]
    img = Image.open(path).convert('RGB')
    W, H = img.size
    print(f"image: {path}  size={W}x{H}")
    half = W // 2

    bb = bbox_of(img, PLATE, 6, 0, 0, half, H, 2)
    print(f"plate bbox (left half): {bb}")
    if bb:
        minx, miny, maxx, maxy, n = bb
        print(f"  plate w={maxx-minx+1} h={maxy-miny+1}")

    dots = blobs(img, DOTS, 12, 0, 0, half, H, 30)
    print(f"target dots (green): {len(dots)}")
    for c, mnx, mny, mxx, mxy, cx, cy in dots[:8]:
        print(f"  px={c} bbox=({mnx},{mny})-({mxx},{mxy}) center=({cx:.1f},{cy:.1f})")

    sel = blobs(img, SEL, 25, 0, 0, half, H, 200)
    print(f"selection fill (amber): {len(sel)}")
    for c, mnx, mny, mxx, mxy, cx, cy in sel[:4]:
        print(f"  px={c} bbox=({mnx},{mny})-({mxx},{mxy}) center=({cx:.1f},{cy:.1f}) w={mxx-mnx+1} h={mxy-mny+1}")


main()
