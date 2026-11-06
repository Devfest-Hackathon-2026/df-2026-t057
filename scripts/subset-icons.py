"""Regenerate the checked-in Google icon font with fontTools[woff] installed."""
from pathlib import Path
from fontTools import subset
from fontTools.ttLib import TTFont
from fontTools.varLib.instancer import instantiateVariableFont

icons = """space_dashboard calendar_month grass tune all_inclusive add play_circle
north_east arrow_forward schedule edit_calendar play_arrow task_alt auto_awesome
error assignment flare casino notifications add_circle arrow_upward
login logout settings expand_more person person_add chevron_right forum send psychology""".split()

font = TTFont("node_modules/material-symbols/material-symbols-rounded.woff2")
instantiateVariableFont(font, {"opsz": 24, "wght": 400, "FILL": 0, "GRAD": 0}, inplace=True)
options = subset.Options()
options.layout_closure = False
subsetter = subset.Subsetter(options=options)
subsetter.populate(glyphs=icons, text="".join(icons))
subsetter.subset(font)
font.flavor = "woff2"
font.save("web/public/material-symbols-rounded.woff2")
Path("web/public/material-symbols-LICENSE.txt").write_text(
    Path("node_modules/material-symbols/LICENSE").read_text()
)
