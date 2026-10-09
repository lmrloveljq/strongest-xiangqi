# marks.py —— 在真实窗口截图上找出棋盘高亮块（悬停/选中/落点/上一步），并反算到棋格
import sys
from PIL import Image

PLATE = (0xF0, 0xE0, 0xBC)
CAND = {
    'hover(浅蓝)':   ((166, 187, 204), 20, 120),   # LastFrom 0x6D9ED8 @ A=0x90 over plate
    'select(琥珀)':  ((243, 210, 114), 22, 120),   # SelectFill 0xF4CE5A @ A=0xC0 over plate
    'targetdot(绿)': ((82, 143, 99),   24, 25),    # TargetDot 0x2E7D4F @ A=0xD0 over plate
    'lastTo(深蓝)':  ((147, 170, 198), 20, 120),   # LastTo 0x467EC6 @ A=0xC8 over plate
}


def near(p, c, tol):
    return abs(p[0]-c[0]) <= tol and abs(p[1]-c[1]) <= tol and abs(p[2]-c[2]) <= tol


def bbox(img, color, tol, region):
    x0, y0, x1, y1 = region
    px = img.load()
    mnx, mny, mxx, mxy, n = 10**9, 10**9, -1, -1, 0
    for y in range(y0, y1):
        for x in range(x0, x1):
            if near(px[x, y], color, tol):
                n += 1
                mnx = min(mnx, x); mxx = max(mxx, x)
                mny = min(mny, y); mxy = max(mxy, y)
    if n == 0:
        return None
    return mnx, mny, mxx, mxy, n


def main():
    path = sys.argv[1]
    img = Image.open(path).convert('RGB')
    W, H = img.size
    # 用底板颜色定位棋盘
    pb = bbox(img, PLATE, 6, (0, 0, W // 2, H))
    bx0, by0, bx1, by1, _ = pb
    pw, ph = bx1 - bx0 + 1, by1 - by0 + 1
    cellx = pw / 8.56
    celly = ph / 9.56
    cell = (cellx + celly) / 2
    ox = bx0 + 0.28 * cell
    oy = by0 + 0.28 * cell
    print(f"{path.split(chr(92))[-1]}  {W}x{H}")
    print(f"  plate=({bx0},{by0})-({bx1},{by1}) cell={cell:.2f} origin=({ox:.2f},{oy:.2f})")

    def to_sq(cx, cy):
        f = round((cx - ox) / cell)
        r = 9 - round((cy - oy) / cell)
        return f, r

    letters = "abcdefghi"
    for name, (col, tol, minpx) in CAND.items():
        r = bbox(img, col, tol, (max(0, bx0-14), max(0, by0-14), min(W, bx1+14), min(H, by1+14)))
        if not r:
            print(f"  {name}: 未检出")
            continue
        mnx, mny, mxx, mxy, n = r
        f, rk = to_sq((mnx+mxx)/2, (mny+mxy)/2)
        coord = f"{letters[f]}{rk}" if 0 <= f <= 8 and 0 <= rk <= 9 else "out"
        print(f"  {name}: px={n:6d} bbox=({mnx},{mny})-({mxx},{mxy}) {mxx-mnx+1}x{mxy-mny+1} 中心格={coord}")


main()
