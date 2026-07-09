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
header{display:flex;align-items:center;gap:18px;margin-bottom:8px}
header img{width:72px;height:72px;border-radius:18px}
h1{margin:0;font-size:2rem}h1 small{display:block;font-size:.95rem;color:var(--muted);font-weight:400}
.badge{display:inline-flex;gap:8px;align-items:center;background:var(--accent-dim);
border:1px solid var(--accent);color:var(--accent);border-radius:999px;padding:4px 14px;
font-weight:600;margin:14px 0 4px}
h2{margin:44px 0 14px;font-size:1.25rem;border-bottom:1px solid var(--line);padding-bottom:8px}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(260px,1fr));gap:14px}
.card{background:var(--panel);border:1px solid var(--line);border-radius:var(--radius);padding:18px}
.card h3{margin:0 0 10px;font-size:1.02rem;display:flex;gap:8px;align-items:center}
.card a{display:block;color:var(--accent);text-decoration:none;padding:3px 0;font-size:.92rem}
.card a:hover{text-decoration:underline}
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

    page = f"""<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Sloth Clash — downloads &amp; package repositories</title>
<meta name="description" content="Sloth Clash — Clash Meta (Mihomo) desktop client. Downloads and Linux package repositories.">
<style>{CSS}</style></head><body><div class="wrap">
<header><img src="logo.png" alt="Sloth Clash">
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
<a href="{DL}/SlothClash-linux-amd64.AppImage">AppImage</a>
<a href="{DL}/sloth-clash-linux-amd64.deb">.deb</a>
<a href="{DL}/sloth-clash-linux-amd64.rpm">.rpm</a>
<a href="{DL}/sloth-clash-linux-amd64.pkg.tar.zst">Arch package</a>
<a href="{DL}/sloth-clash-linux-amd64.tar.gz">tar.gz</a></div>
<div class="card"><h3>🐧 Linux ARM64</h3>
<a href="{DL}/SlothClash-linux-arm64.AppImage">AppImage</a>
<a href="{DL}/sloth-clash-linux-arm64.deb">.deb</a>
<a href="{DL}/sloth-clash-linux-arm64.rpm">.rpm</a>
<a href="{DL}/sloth-clash-linux-arm64.pkg.tar.zst">Arch package</a>
<a href="{DL}/sloth-clash-linux-arm64.tar.gz">tar.gz</a></div>
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
<h3 style="margin:18px 0 8px">Arch Linux</h3>
<pre>sudo pacman -U {DL}/sloth-clash-linux-amd64.pkg.tar.zst</pre>

<h2>Releases</h2>
{releases_html}

<footer>🦥 <a href="https://github.com/{REPO}">GitHub</a> ·
<a href="https://t.me/nemux_dev">Telegram</a> ·
<a href="https://github.com/{REPO}/releases">All releases</a> · GPL-3.0</footer>
</div><script>{JS}</script></body></html>
"""
    with open(f"{args.out}/index.html", "w", encoding="utf-8") as f:
        f.write(page)
    print(f"wrote {args.out}/index.html ({len(page)} bytes, {len(rels)} releases)")


if __name__ == "__main__":
    main()
