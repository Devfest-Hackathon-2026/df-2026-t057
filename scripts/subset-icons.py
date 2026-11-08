"""管理画面で使うアイコンだけを含むフォントを作り直す（fontTools[woff] が必要）。

アイコンを追加したら icons に名前を足し、リポジトリのルートで実行する:
    python3 scripts/subset-icons.py
"""
from pathlib import Path
from fontTools import subset
from fontTools.ttLib import TTFont
from fontTools.varLib.instancer import instantiateVariableFont

WEB = Path("apps/web")
SOURCE = WEB / "node_modules/material-symbols"
PUBLIC = WEB / "public"

icons = """add add_circle add_task arrow_drop_down arrow_upward assignment auto_awesome bedtime
calendar_month calendar_today check chevron_left chevron_right close delete edit
edit_calendar error event_available expand_more flare forum grass help info link
logout mobile more_vert motion_photos_off notifications open_in_new palette person person_add
play_arrow psychology schedule send settings snooze task_alt texture timer volume_up""".split()

font = TTFont(SOURCE / "material-symbols-rounded.woff2")
instantiateVariableFont(font, {"opsz": 24, "wght": 400, "FILL": 0, "GRAD": 0}, inplace=True)
options = subset.Options()
options.layout_closure = False
subsetter = subset.Subsetter(options=options)
subsetter.populate(glyphs=icons, text="".join(icons))
subsetter.subset(font)
font.flavor = "woff2"
font.save(PUBLIC / "material-symbols-rounded.woff2")
(PUBLIC / "material-symbols-LICENSE.txt").write_text((SOURCE / "LICENSE").read_text())
