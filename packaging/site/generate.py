#!/usr/bin/env python3
"""Generates the SlothClash packages landing page (GitHub Pages).

Run by .github/workflows/package-repos.yml on every deploy. Pulls the recent
releases via `gh` (GH_TOKEN provided by the workflow) so the page always shows
the latest version + release notes without manual edits. Self-contained output:
inline CSS/JS, no external assets except the repo logo copied alongside.

Usage: python3 packaging/site/generate.py --out site
"""

import argparse
import html
import json
import re
import subprocess
from datetime import datetime

REPO = "Nemu-x/SlothClash"
PAGES = f"https://nemu-x.github.io/SlothClash"
DL = f"https://github.com/{REPO}/releases/latest/download"


def gh_releases(limit=5):
    try:
        out = subprocess.run(
            ["gh", "release", "list", "--repo", REPO, "--limit", str(limit),
             "--json", "tagName,publishedAt,name"],
            capture_output=True, text=True, check=True).stdout
        rels = json.loads(out)
    except Exception:
        return []
    for r in rels:
        # One broken release must not blank the whole feed.
        try:
            body = subprocess.run(
                ["gh", "release", "view", r["tagName"], "--repo", REPO,
                 "--json", "body"],
                capture_output=True, text=True, check=True).stdout
            r["body"] = json.loads(body).get("body", "")
        except Exception:
            r["body"] = ""
    return rels


def md_lite(text):
    """Tiny, safe markdown subset for release notes: headings, bullets, links, code."""
    out = []
    in_list = False
    for raw in text.splitlines():
        line = html.escape(raw.rstrip())
        line = re.sub(r"`([^`]+)`", r"<code>\1</code>", line)
        line = re.sub(r"\*\*([^*]+)\*\*", r"<strong>\1</strong>", line)
        line = re.sub(r"\[([^\]]+)\]\((https?://[^)]+)\)", r'<a href="\2">\1</a>', line)
        # bare PR/compare URLs -> short links
        line = re.sub(r"(?<!href=\")(https?://github\.com/\S+)", r'<a href="\1">\1</a>', line)
        if re.match(r"^\s*[-*] ", line):
            if not in_list:
                out.append("<ul>")
                in_list = True
            out.append("<li>" + re.sub(r"^\s*[-*] ", "", line) + "</li>")
            continue
        if in_list:
            out.append("</ul>")
            in_list = False
        if line.startswith("### "):
            out.append(f"<h4>{line[4:]}</h4>")
        elif line.startswith("## "):
            out.append(f"<h4>{line[3:]}</h4>")
        elif line.strip():
            out.append(f"<p>{line}</p>")
    if in_list:
        out.append("</ul>")
    return "\n".join(out)


def fmt_date(iso):
    try:
        return datetime.fromisoformat(iso.replace("Z", "+00:00")).strftime("%d %b %Y")
    except Exception:
        return ""


CSS = """
:root{--bg:#14120f;--panel:#1e1c1a;--panel2:#181614;--text:#f2efe8;--muted:#a39e96;
--accent:#c9a86c;--accent-dim:rgba(201,168,108,.18);--line:rgba(255,255,255,.09);--radius:14px}
*{box-sizing:border-box}body{margin:0;background:
radial-gradient(1200px 500px at 70% -10%,rgba(201,168,108,.08),transparent 60%),var(--bg);
color:var(--text);font:16px/1.6 system-ui,'Segoe UI',sans-serif}
.wrap{max-width:960px;margin:0 auto;padding:48px 20px 80px}
header{display:flex;align-items:center;gap:18px;margin-bottom:8px;position:relative}
.hdrlinks{position:absolute;top:0;right:0;display:flex;gap:10px}
.hdrlinks a{display:flex;align-items:center;justify-content:center;width:40px;height:40px;
border-radius:12px;background:var(--panel);border:1px solid var(--line);color:var(--accent)}
.hdrlinks a:hover{background:var(--accent-dim);border-color:var(--accent)}
.hdrlinks svg{width:20px;height:20px;fill:currentColor}
header img{width:72px;height:72px;border-radius:18px}
h1{margin:0;font-size:2rem}h1 small{display:block;font-size:.95rem;color:var(--muted);font-weight:400}
.badge{display:inline-flex;gap:8px;align-items:center;background:var(--accent-dim);
border:1px solid var(--accent);color:var(--accent);border-radius:999px;padding:4px 14px;
font-weight:600;margin:14px 0 4px}
h2{margin:44px 0 14px;font-size:1.25rem;border-bottom:1px solid var(--line);padding-bottom:8px}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(260px,1fr));gap:14px}
.card{background:var(--panel);border:1px solid var(--line);border-radius:var(--radius);padding:18px}
.card h3{margin:0 0 10px;font-size:1.02rem;display:flex;gap:8px;align-items:center}
.card a{display:flex;align-items:center;gap:8px;color:var(--accent);text-decoration:none;padding:3px 0;font-size:.92rem}
.card a:hover{text-decoration:underline}
.card a .pk{width:16px;height:16px;flex:none}
pre{position:relative;background:var(--panel2);border:1px solid var(--line);border-radius:10px;
padding:14px 46px 14px 14px;overflow-x:auto;font-size:.85rem;line-height:1.55;color:#e8e2d6}
.copy{position:absolute;top:8px;right:8px;background:var(--accent-dim);border:1px solid var(--accent);
color:var(--accent);border-radius:8px;padding:2px 10px;font-size:.78rem;cursor:pointer}
.copy:hover{background:var(--accent);color:#14120f}
.rel{background:var(--panel);border:1px solid var(--line);border-radius:var(--radius);
padding:18px 20px;margin-bottom:14px}
.rel h3{margin:0;display:flex;justify-content:space-between;align-items:baseline;font-size:1.05rem}
.rel h3 span{color:var(--muted);font-size:.85rem;font-weight:400}
.rel .notes{color:#d9d4c9;font-size:.92rem}.rel .notes h4{margin:12px 0 4px;color:var(--text)}
.rel .notes ul{margin:6px 0;padding-left:22px}.rel .notes code{background:var(--panel2);
padding:1px 6px;border-radius:6px;font-size:.85em}
.rel .notes a{color:var(--accent);word-break:break-all}
footer{margin-top:56px;color:var(--muted);font-size:.9rem;border-top:1px solid var(--line);padding-top:18px}
footer a{color:var(--accent);text-decoration:none}.muted{color:var(--muted)}
@media(max-width:560px){header{flex-direction:column;text-align:center}}
"""

JS = """
document.querySelectorAll('pre').forEach(p=>{const b=document.createElement('button');
b.className='copy';b.textContent='copy';b.onclick=()=>{navigator.clipboard.writeText(p.innerText.trim());
b.textContent='done!';setTimeout(()=>b.textContent='copy',1500)};p.appendChild(b)});
"""


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default="site")
    args = ap.parse_args()

    rels = gh_releases()
    latest = rels[0] if rels else None
    ver = latest["tagName"].lstrip("v") if latest else "—"
    date = fmt_date(latest["publishedAt"]) if latest else ""

    releases_html = "".join(
        f'<div class="rel"><h3>{html.escape(r.get("name") or r["tagName"])}'
        f'<span>{fmt_date(r["publishedAt"])}</span></h3>'
        f'<div class="notes">{md_lite(r.get("body") or "")}</div></div>'
        for r in rels
    ) or '<p class="muted">Release feed unavailable.</p>'

    # Official distro logos (simple-icons) inlined so the page stays self-contained.
    # AppImage runs on any distro, so it gets the generic Linux (Tux) mark; tar.gz
    # is a raw archive, not an OS, so it keeps a neutral box glyph.
    def _ic(color: str, path: str) -> str:
        return f'<svg viewBox="0 0 24 24" class="pk"><path fill="{color}" d="{path}"/></svg>'

    IC_APPIMAGE = _ic("#FCC624", "M12.504 0c-.155 0-.315.008-.48.021-4.226.333-3.105 4.807-3.17 6.298-.076 1.092-.3 1.953-1.05 3.02-.885 1.051-2.127 2.75-2.716 4.521-.278.832-.41 1.684-.287 2.489a.424.424 0 00-.11.135c-.26.268-.45.6-.663.839-.199.199-.485.267-.797.4-.313.136-.658.269-.864.68-.09.189-.136.394-.132.602 0 .199.027.4.055.536.058.399.116.728.04.97-.249.68-.28 1.145-.106 1.484.174.334.535.47.94.601.81.2 1.91.135 2.774.6.926.466 1.866.67 2.616.47.526-.116.97-.464 1.208-.946.587-.003 1.23-.269 2.26-.334.699-.058 1.574.267 2.577.2.025.134.063.198.114.333l.003.003c.391.778 1.113 1.132 1.884 1.071.771-.06 1.592-.536 2.257-1.306.631-.765 1.683-1.084 2.378-1.503.348-.199.629-.469.649-.853.023-.4-.2-.811-.714-1.376v-.097l-.003-.003c-.17-.2-.25-.535-.338-.926-.085-.401-.182-.786-.492-1.046h-.003c-.059-.054-.123-.067-.188-.135a.357.357 0 00-.19-.064c.431-1.278.264-2.55-.173-3.694-.533-1.41-1.465-2.638-2.175-3.483-.796-1.005-1.576-1.957-1.56-3.368.026-2.152.236-6.133-3.544-6.139zm.529 3.405h.013c.213 0 .396.062.584.198.19.135.33.332.438.533.105.259.158.459.166.724 0-.02.006-.04.006-.06v.105a.086.086 0 01-.004-.021l-.004-.024a1.807 1.807 0 01-.15.706.953.953 0 01-.213.335.71.71 0 00-.088-.042c-.104-.045-.198-.064-.284-.133a1.312 1.312 0 00-.22-.066c.05-.06.146-.133.183-.198.053-.128.082-.264.088-.402v-.02a1.21 1.21 0 00-.061-.4c-.045-.134-.101-.2-.183-.333-.084-.066-.167-.132-.267-.132h-.016c-.093 0-.176.03-.262.132a.8.8 0 00-.205.334 1.18 1.18 0 00-.09.4v.019c.002.089.008.179.02.267-.193-.067-.438-.135-.607-.202a1.635 1.635 0 01-.018-.2v-.02a1.772 1.772 0 01.15-.768c.082-.22.232-.406.43-.533a.985.985 0 01.594-.2zm-2.962.059h.036c.142 0 .27.048.399.135.146.129.264.288.344.465.09.199.14.4.153.667v.004c.007.134.006.2-.002.266v.08c-.03.007-.056.018-.083.024-.152.055-.274.135-.393.2.012-.09.013-.18.003-.267v-.015c-.012-.133-.04-.2-.082-.333a.613.613 0 00-.166-.267.248.248 0 00-.183-.064h-.021c-.071.006-.13.04-.186.132a.552.552 0 00-.12.27.944.944 0 00-.023.33v.015c.012.135.037.2.08.334.046.134.098.2.166.268.01.009.02.018.034.024-.07.057-.117.07-.176.136a.304.304 0 01-.131.068 2.62 2.62 0 01-.275-.402 1.772 1.772 0 01-.155-.667 1.759 1.759 0 01.08-.668 1.43 1.43 0 01.283-.535c.128-.133.26-.2.418-.2zm1.37 1.706c.332 0 .733.065 1.216.399.293.2.523.269 1.052.468h.003c.255.136.405.266.478.399v-.131a.571.571 0 01.016.47c-.123.31-.516.643-1.063.842v.002c-.268.135-.501.333-.775.465-.276.135-.588.292-1.012.267a1.139 1.139 0 01-.448-.067 3.566 3.566 0 01-.322-.198c-.195-.135-.363-.332-.612-.465v-.005h-.005c-.4-.246-.616-.512-.686-.71-.07-.268-.005-.47.193-.6.224-.135.38-.271.483-.336.104-.074.143-.102.176-.131h.002v-.003c.169-.202.436-.47.839-.601.139-.036.294-.065.466-.065zm2.8 2.142c.358 1.417 1.196 3.475 1.735 4.473.286.534.855 1.659 1.102 3.024.156-.005.33.018.513.064.646-1.671-.546-3.467-1.089-3.966-.22-.2-.232-.335-.123-.335.59.534 1.365 1.572 1.646 2.757.13.535.16 1.104.021 1.67.067.028.135.06.205.067 1.032.534 1.413.938 1.23 1.537v-.043c-.06-.003-.12 0-.18 0h-.016c.151-.467-.182-.825-1.065-1.224-.915-.4-1.646-.336-1.77.465-.008.043-.013.066-.018.135-.068.023-.139.053-.209.064-.43.268-.662.669-.793 1.187-.13.533-.17 1.156-.205 1.869v.003c-.02.334-.17.838-.319 1.35-1.5 1.072-3.58 1.538-5.348.334a2.645 2.645 0 00-.402-.533 1.45 1.45 0 00-.275-.333c.182 0 .338-.03.465-.067a.615.615 0 00.314-.334c.108-.267 0-.697-.345-1.163-.345-.467-.931-.995-1.788-1.521-.63-.4-.986-.87-1.15-1.396-.165-.534-.143-1.085-.015-1.645.245-1.07.873-2.11 1.274-2.763.107-.065.037.135-.408.974-.396.751-1.14 2.497-.122 3.854a8.123 8.123 0 01.647-2.876c.564-1.278 1.743-3.504 1.836-5.268.048.036.217.135.289.202.218.133.38.333.59.465.21.201.477.335.876.335.039.003.075.006.11.006.412 0 .73-.134.997-.268.29-.134.52-.334.74-.4h.005c.467-.135.835-.402 1.044-.7zm2.185 8.958c.037.6.343 1.245.882 1.377.588.134 1.434-.333 1.791-.765l.211-.01c.315-.007.577.01.847.268l.003.003c.208.199.305.53.391.876.085.4.154.78.409 1.066.486.527.645.906.636 1.14l.003-.007v.018l-.003-.012c-.015.262-.185.396-.498.595-.63.401-1.746.712-2.457 1.57-.618.737-1.37 1.14-2.036 1.191-.664.053-1.237-.2-1.574-.898l-.005-.003c-.21-.4-.12-1.025.056-1.69.176-.668.428-1.344.463-1.897.037-.714.076-1.335.195-1.814.12-.465.308-.797.641-.984l.045-.022zm-10.814.049h.01c.053 0 .105.005.157.014.376.055.706.333 1.023.752l.91 1.664.003.003c.243.533.754 1.064 1.189 1.637.434.598.77 1.131.729 1.57v.006c-.057.744-.48 1.148-1.125 1.294-.645.135-1.52.002-2.395-.464-.968-.536-2.118-.469-2.857-.602-.369-.066-.61-.2-.723-.4-.11-.2-.113-.602.123-1.23v-.004l.002-.003c.117-.334.03-.752-.027-1.118-.055-.401-.083-.71.043-.94.16-.334.396-.4.69-.533.294-.135.64-.202.915-.47h.002v-.002c.256-.268.445-.601.668-.838.19-.201.38-.336.663-.336z")
    IC_DEB = _ic("#A81D33", "M13.88 12.685c-.4 0 .08.2.601.28.14-.1.27-.22.39-.33a3.001 3.001 0 01-.99.05m2.14-.53c.23-.33.4-.69.47-1.06-.06.27-.2.5-.33.73-.75.47-.07-.27 0-.56-.8 1.01-.11.6-.14.89m.781-2.05c.05-.721-.14-.501-.2-.221.07.04.13.5.2.22M12.38.31c.2.04.45.07.42.12.23-.05.28-.1-.43-.12m.43.12l-.15.03.14-.01V.43m6.633 9.944c.02.64-.2.95-.38 1.5l-.35.181c-.28.54.03.35-.17.78-.44.39-1.34 1.22-1.62 1.301-.201 0 .14-.25.19-.34-.591.4-.481.6-1.371.85l-.03-.06c-2.221 1.04-5.303-1.02-5.253-3.842-.03.17-.07.13-.12.2a3.551 3.552 0 012.001-3.501 3.361 3.362 0 013.732.48 3.341 3.342 0 00-2.721-1.3c-1.18.01-2.281.76-2.651 1.57-.6.38-.67 1.47-.93 1.661-.361 2.601.66 3.722 2.38 5.042.27.19.08.21.12.35a4.702 4.702 0 01-1.53-1.16c.23.33.47.66.8.91-.55-.18-1.27-1.3-1.48-1.35.93 1.66 3.78 2.921 5.261 2.3a6.203 6.203 0 01-2.33-.28c-.33-.16-.77-.51-.7-.57a5.802 5.803 0 005.902-.84c.44-.35.93-.94 1.07-.95-.2.32.04.16-.12.44.44-.72-.2-.3.46-1.24l.24.33c-.09-.6.74-1.321.66-2.262.19-.3.2.3 0 .97.29-.74.08-.85.15-1.46.08.2.18.42.23.63-.18-.7.2-1.2.28-1.6-.09-.05-.28.3-.32-.53 0-.37.1-.2.14-.28-.08-.05-.26-.32-.38-.861.08-.13.22.33.34.34-.08-.42-.2-.75-.2-1.08-.34-.68-.12.1-.4-.3-.34-1.091.3-.25.34-.74.54.77.84 1.96.981 2.46-.1-.6-.28-1.2-.49-1.76.16.07-.26-1.241.21-.37A7.823 7.824 0 0017.702 1.6c.18.17.42.39.33.42-.75-.45-.62-.48-.73-.67-.61-.25-.65.02-1.06 0C15.082.73 14.862.8 13.8.4l.05.23c-.77-.25-.9.1-1.73 0-.05-.04.27-.14.53-.18-.741.1-.701-.14-1.431.03.17-.13.36-.21.55-.32-.6.04-1.44.35-1.18.07C9.6.68 7.847 1.3 6.867 2.22L6.838 2c-.45.54-1.96 1.611-2.08 2.311l-.131.03c-.23.4-.38.85-.57 1.261-.3.52-.45.2-.4.28-.6 1.22-.9 2.251-1.16 3.102.18.27 0 1.65.07 2.76-.3 5.463 3.84 10.776 8.363 12.006.67.23 1.65.23 2.49.25-.99-.28-1.12-.15-2.08-.49-.7-.32-.85-.7-1.34-1.13l.2.35c-.971-.34-.57-.42-1.361-.67l.21-.27c-.31-.03-.83-.53-.97-.81l-.34.01c-.41-.501-.63-.871-.61-1.161l-.111.2c-.13-.21-1.52-1.901-.8-1.511-.13-.12-.31-.2-.5-.55l.14-.17c-.35-.44-.64-1.02-.62-1.2.2.24.32.3.45.33-.88-2.172-.93-.12-1.601-2.202l.15-.02c-.1-.16-.18-.34-.26-.51l.06-.6c-.63-.74-.18-3.102-.09-4.402.07-.54.53-1.1.88-1.981l-.21-.04c.4-.71 2.341-2.872 3.241-2.761.43-.55-.09 0-.18-.14.96-.991 1.26-.7 1.901-.88.7-.401-.6.16-.27-.151 1.2-.3.85-.7 2.421-.85.16.1-.39.14-.52.26 1-.49 3.151-.37 4.562.27 1.63.77 3.461 3.011 3.531 5.132l.08.02c-.04.85.13 1.821-.17 2.711l.2-.42M9.54 13.236l-.05.28c.26.35.47.73.8 1.01-.24-.47-.42-.66-.75-1.3m.62-.02c-.14-.15-.22-.34-.31-.52.08.32.26.6.43.88l-.12-.36m10.945-2.382l-.07.15c-.1.76-.34 1.511-.69 2.212.4-.73.65-1.541.75-2.362M12.45.12c.27-.1.66-.05.95-.12-.37.03-.74.05-1.1.1l.15.02M3.006 5.142c.07.57-.43.8.11.42.3-.66-.11-.18-.1-.42m-.64 2.661c.12-.39.15-.62.2-.84-.35.44-.17.53-.2.83")
    IC_RPM = _ic("#51A2DA", "M12.001 0C5.376 0 .008 5.369.004 11.992H.002v9.287h.002A2.726 2.726 0 0 0 2.73 24h9.275c6.626-.004 11.993-5.372 11.993-11.997C23.998 5.375 18.628 0 12 0zm2.431 4.94c2.015 0 3.917 1.543 3.917 3.671 0 .197.001.395-.03.619a1.002 1.002 0 0 1-1.137.893 1.002 1.002 0 0 1-.842-1.175 2.61 2.61 0 0 0 .013-.337c0-1.207-.987-1.672-1.92-1.672-.934 0-1.775.784-1.777 1.672.016 1.027 0 2.046 0 3.07l1.732-.012c1.352-.028 1.368 2.009.016 1.998l-1.748.013c-.004.826.006.677.002 1.093 0 0 .015 1.01-.016 1.776-.209 2.25-2.124 4.046-4.424 4.046-2.438 0-4.448-1.993-4.448-4.437.073-2.515 2.078-4.492 4.603-4.469l1.409-.01v1.996l-1.409.013h-.007c-1.388.04-2.577.984-2.6 2.47a2.438 2.438 0 0 0 2.452 2.439c1.356 0 2.441-.987 2.441-2.437l-.001-7.557c0-.14.005-.252.02-.407.23-1.848 1.883-3.256 3.754-3.256z")
    IC_ARCH = _ic("#1793D1", "M11.39.605C10.376 3.092 9.764 4.72 8.635 7.132c.693.734 1.543 1.589 2.923 2.554-1.484-.61-2.496-1.224-3.252-1.86C6.86 10.842 4.596 15.138 0 23.395c3.612-2.085 6.412-3.37 9.021-3.862a6.61 6.61 0 01-.171-1.547l.003-.115c.058-2.315 1.261-4.095 2.687-3.973 1.426.12 2.534 2.096 2.478 4.409a6.52 6.52 0 01-.146 1.243c2.58.505 5.352 1.787 8.914 3.844-.702-1.293-1.33-2.459-1.929-3.57-.943-.73-1.926-1.682-3.933-2.713 1.38.359 2.367.772 3.137 1.234-6.09-11.334-6.582-12.84-8.67-17.74z")
    IC_TARGZ = ('<svg viewBox="0 0 24 24" class="pk"><path fill="#9aa0a6" d="M3 5h18v4H3z"/>'
                '<path fill="#7a8087" d="M4 10h16v9H4z"/><path fill="#3c4043" d="M10 5h4v7h-4z"/></svg>')

    page = f"""<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Sloth Clash — downloads &amp; package repositories</title>
<meta name="description" content="Sloth Clash — Clash Meta (Mihomo) desktop client. Downloads and Linux package repositories.">
<style>{CSS}</style></head><body><div class="wrap">
<header>
<div class="hdrlinks">
<a href="https://github.com/{REPO}/wiki" title="Wiki / Docs" aria-label="Wiki / Docs"><svg viewBox="0 0 24 24"><path d="M6 2h11a2 2 0 0 1 2 2v16a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2zm0 2v16h11V4H6zm2 3h7v2H8V7zm0 4h7v2H8v-2z"/></svg></a>
<a href="https://t.me/nemux_dev" title="Telegram group" aria-label="Telegram group"><svg viewBox="0 0 24 24"><path d="M9.78 18.65l.28-4.23 7.68-6.92c.34-.31-.07-.46-.52-.19L7.74 13.3 3.64 12c-.88-.25-.89-.86.2-1.3l15.97-6.16c.73-.33 1.43.18 1.15 1.3l-2.72 12.81c-.19.91-.74 1.13-1.5.71L12.6 16.3l-1.99 1.93c-.23.23-.42.42-.83.42z"/></svg></a>
<a href="https://github.com/Nemu-x/SlothClash" title="GitHub" aria-label="GitHub"><svg viewBox="0 0 24 24"><path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z"/></svg></a>
</div>
<img src="logo.png" alt="Sloth Clash">
<div><h1>Sloth Clash<small>Clash Meta (Mihomo) desktop client — Windows · macOS · Linux</small></h1>
<div class="badge">🦥 latest: v{html.escape(ver)}{' · ' + date if date else ''}</div></div></header>

<h2>Downloads</h2>
<div class="grid">
<div class="card"><h3>🪟 Windows</h3>
<a href="{DL}/SlothClash-windows-amd64-installer.exe">Installer (x64)</a>
<a href="{DL}/SlothClash-windows-arm64-installer.exe">Installer (ARM64)</a></div>
<div class="card"><h3>🍎 macOS</h3>
<a href="{DL}/SlothClash-macOS-arm64.dmg">Apple Silicon (.dmg)</a>
<a href="{DL}/SlothClash-macOS-x64.dmg">Intel (.dmg)</a></div>
<div class="card"><h3>🐧 Linux x64</h3>
<a href="{DL}/SlothClash-linux-amd64.AppImage">{IC_APPIMAGE}AppImage</a>
<a href="{DL}/sloth-clash-linux-amd64.deb">{IC_DEB}.deb</a>
<a href="{DL}/sloth-clash-linux-amd64.rpm">{IC_RPM}.rpm</a>
<a href="{DL}/sloth-clash-linux-amd64.pkg.tar.zst">{IC_ARCH}Arch package</a>
<a href="{DL}/sloth-clash-linux-amd64.tar.gz">{IC_TARGZ}tar.gz</a></div>
<div class="card"><h3>🐧 Linux ARM64</h3>
<a href="{DL}/SlothClash-linux-arm64.AppImage">{IC_APPIMAGE}AppImage</a>
<a href="{DL}/sloth-clash-linux-arm64.deb">{IC_DEB}.deb</a>
<a href="{DL}/sloth-clash-linux-arm64.rpm">{IC_RPM}.rpm</a>
<a href="{DL}/sloth-clash-linux-arm64.pkg.tar.zst">{IC_ARCH}Arch package</a>
<a href="{DL}/sloth-clash-linux-arm64.tar.gz">{IC_TARGZ}tar.gz</a></div>
</div>
<p class="muted">Verify downloads against <a style="color:var(--accent)" href="{DL}/SHA256SUMS">SHA256SUMS</a>
(minisign-signed: <a style="color:var(--accent)" href="{DL}/SHA256SUMS.minisig">.minisig</a>).</p>

<h2>Install via package manager (auto-updates)</h2>
<h3 style="margin:18px 0 8px">Debian / Ubuntu</h3>
<pre>curl -fsSL {PAGES}/apt/key.gpg | sudo tee /usr/share/keyrings/slothclash.gpg &gt;/dev/null
echo "deb [signed-by=/usr/share/keyrings/slothclash.gpg] {PAGES}/apt ./" | sudo tee /etc/apt/sources.list.d/slothclash.list
sudo apt update &amp;&amp; sudo apt install sloth-clash</pre>
<h3 style="margin:18px 0 8px">Fedora / openSUSE</h3>
<pre>sudo dnf config-manager --add-repo {PAGES}/rpm/slothclash.repo
sudo dnf install sloth-clash</pre>
<h3 style="margin:18px 0 8px">Arch Linux (AUR)</h3>
<pre>yay -S sloth-clash-bin      # or: paru -S sloth-clash-bin</pre>
<p class="muted">Or a one-off install without an AUR helper:
<code>sudo pacman -U {DL}/sloth-clash-linux-amd64.pkg.tar.zst</code></p>

<h2>Releases</h2>
{releases_html}

<footer>🦥 <a href="https://github.com/{REPO}">GitHub</a> ·
<a href="https://github.com/{REPO}/wiki">Wiki</a> ·
<a href="https://t.me/nemux_dev">Telegram</a> ·
<a href="https://github.com/{REPO}/releases">All releases</a> · GPL-3.0</footer>
</div><script>{JS}</script></body></html>
"""
    with open(f"{args.out}/index.html", "w", encoding="utf-8") as f:
        f.write(page)
    print(f"wrote {args.out}/index.html ({len(page)} bytes, {len(rels)} releases)")


if __name__ == "__main__":
    main()
