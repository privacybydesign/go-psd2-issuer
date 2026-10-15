"""Generates the irma-demo.enablenow issuer and credential definitions.

Usage: python3 scheme/generate.py <scheme-root> [--logos]

Writes <scheme-root>/irma-demo/enablenow/... (or <scheme-root>/enablenow/...
when <scheme-root> is itself the irma-demo scheme, i.e. contains
description.xml). The credentials and attributes come from
backend/credentials/definitions.json, the same file the backend uses;
credential directories that are no longer defined are removed.
"""
import json
import math
import os
import shutil
import struct
import sys
import zlib
from xml.sax.saxutils import escape as e

DEFINITIONS = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "backend", "credentials", "definitions.json")

ISSUER = "enablenow"
CATEGORY = ("Finance", "Financieel")
COLORS = ("#ffffff", "#0B4F3C", "#167A5B")

AMOUNT = (" (net average per month in whole euros; positive is received, negative is paid)",
          " (netto gemiddelde per maand in hele euro's; positief is ontvangen, negatief is betaald)")

HOWTO = ("1. Log in to your bank through enablenow and give permission to read your transactions.\\n"
         "2. Your transactions of the last 12 months are categorized automatically.\\n"
         "3. Check the results and choose which cards to add to your Yivi app.",
         "1. Log in bij je bank via enablenow en geef toestemming om je transacties te lezen.\\n"
         "2. Je transacties van de afgelopen 12 maanden worden automatisch gecategoriseerd.\\n"
         "3. Controleer de resultaten en kies welke kaartjes je aan je Yivi-app toevoegt.")

PURPOSE = ("With financial data in your Yivi app you can prove facts about your finances, for example to a landlord or lender, without handing over your bank statements.",
           "Met financiële gegevens in je Yivi-app kun je feiten over je financiën aantonen, bijvoorbeeld aan een verhuurder of geldverstrekker, zonder je bankafschriften af te geven.")


def pair(t):
    return (t["en"], t["nl"])


def lower_first(s):
    # keep acronyms such as AOW or IBAN
    return s if s[1:2].isupper() else s[0].lower() + s[1:]


def load_credentials():
    """Returns {key: (name, description, faq intro, singleton, attributes)}."""
    with open(DEFINITIONS) as f:
        defs = json.load(f)
    period = [(a["id"], *pair(a["name"]), *pair(a["description"])) for a in defs["period_attributes"]]
    out = {}
    for c in defs["credentials"]:
        en, nl = pair(c["name"])
        den, dnl = pair(c["description"])
        e1 = bool(c.get("e1_group"))
        if e1:
            den += ", derived from your bank transactions"
            dnl += ", afgeleid uit je banktransacties"
        attrs = []
        for a in c["attributes"]:
            aen, anl = pair(a["description"])
            if a.get("e1"):
                aen, anl = f"E1 category {a['e1']}: {aen}{AMOUNT[0]}", f"E1-categorie {a['e1']}: {anl}{AMOUNT[1]}"
            attrs.append((a["id"], *pair(a["name"]), aen, anl))
        out[c["key"]] = (
            ("Demo " + en, "Demo " + nl),
            (den, dnl),
            (f"Add {lower_first(den)}, to your Yivi app.", f"Voeg {lower_first(dnl)}, toe aan je Yivi-app."),
            e1,
            attrs + (period if e1 else []),
        )
    return out


def tr(tag, pair, indent="\t"):
    return f"{indent}<{tag}>\n{indent}\t<en>{e(pair[0])}</en>\n{indent}\t<nl>{e(pair[1])}</nl>\n{indent}</{tag}>\n"


def issuer_xml():
    name = ("Demo enablenow", "Demo enablenow")
    return ("<Issuer version=\"4\">\n\t<ID>enablenow</ID>\n" + tr("Name", name) + tr("ShortName", name) +
            "\t<SchemeManager>irma-demo</SchemeManager>\n"
            "\t<ContactAddress>https://www.enablenow.nl</ContactAddress>\n"
            "\t<ContactEMail></ContactEMail>\n"
            "</Issuer>\n")


def credential_xml(cid, name, desc, intro, singleton, attrs):
    content = ("You will add the following data:\\n" + ",\\n".join("• " + lower_first(a[1]) for a in attrs) + ".",
               "Je voegt de volgende gegevens toe:\\n" + ",\\n".join("• " + lower_first(a[2]) for a in attrs) + ".")
    out = "<IssueSpecification version=\"4\">\n" + tr("Name", name) + tr("ShortName", name)
    out += f"\t<SchemeManager>irma-demo</SchemeManager>\n\t<IssuerID>{ISSUER}</IssuerID>\n\t<CredentialID>{cid}</CredentialID>\n"
    out += tr("Description", desc)
    if singleton:
        out += "\t<ShouldBeSingleton>true</ShouldBeSingleton>\n"
    out += (f"\n\t<ForegroundColor>{COLORS[0]}</ForegroundColor>\n"
            f"\t<BackgroundGradientStart>{COLORS[1]}</BackgroundGradientStart>\n"
            f"\t<BackgroundGradientEnd>{COLORS[2]}</BackgroundGradientEnd>\n\n")
    out += tr("Category", CATEGORY) + tr("FAQIntro", intro) + tr("FAQPurpose", PURPOSE)
    out += tr("FAQContent", content) + tr("FAQHowto", HOWTO)
    out += "\n\t<Attributes>\n"
    for i, en, nl, den, dnl in attrs:
        out += (f"\t\t<Attribute id=\"{i}\">\n" + tr("Name", (en, nl), "\t\t\t") +
                tr("Description", (den, dnl), "\t\t\t") + "\t\t</Attribute>\n")
    out += "\t</Attributes>\n</IssueSpecification>\n"
    return out


# --- logos: a minimal PNG renderer, so no imaging library is needed ----------

SIZE = 256


def png(pixels):
    raw = b"".join(b"\x00" + bytes(c for px in row for c in px) for row in pixels)

    def chunk(kind, data):
        return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data) & 0xffffffff)
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", SIZE, SIZE, 8, 6, 0, 0, 0)) +
            chunk(b"IDAT", zlib.compress(raw, 9)) + chunk(b"IEND", b""))


def inside_polygon(x, y, poly):
    hit = False
    for (x1, y1), (x2, y2) in zip(poly, poly[1:] + poly[:1]):
        if (y1 > y) != (y2 > y) and x < (x2 - x1) * (y - y1) / (y2 - y1) + x1:
            hit = not hit
    return hit


def hexrgb(h):
    return tuple(int(h[i:i + 2], 16) for i in (1, 3, 5))


# glyphs as polygons in a 0..1 box
GLYPHS = {
    "income": [[(.5, .18), (.80, .50), (.62, .50), (.62, .82), (.38, .82), (.38, .50), (.20, .50)]],
    "housing": [[(.5, .18), (.84, .48), (.74, .48), (.74, .82), (.26, .82), (.26, .48), (.16, .48)]],
    "loan": [[(.5, .82), (.80, .50), (.62, .50), (.62, .18), (.38, .18), (.38, .50), (.20, .50)]],
    "subsidy": [[(.40, .18), (.60, .18), (.60, .40), (.82, .40), (.82, .60), (.60, .60), (.60, .82),
                  (.40, .82), (.40, .60), (.18, .60), (.18, .40), (.40, .40)]],
    "bankaccount": [[(.5, .16), (.86, .36), (.14, .36)], [(.20, .40), (.30, .40), (.30, .70), (.20, .70)],
                    [(.45, .40), (.55, .40), (.55, .70), (.45, .70)], [(.70, .40), (.80, .40), (.80, .70), (.70, .70)],
                    [(.14, .74), (.86, .74), (.86, .84), (.14, .84)]],
}
GLYPHS[ISSUER] = GLYPHS["bankaccount"]
# three bars, for the credentials without a glyph of their own
GENERIC = [[(.22, .26), (.78, .26), (.78, .38), (.22, .38)], [(.22, .44), (.78, .44), (.78, .56), (.22, .56)],
           [(.22, .62), (.62, .62), (.62, .74), (.22, .74)]]


def logo(key):
    start, end = hexrgb(COLORS[1]), hexrgb(COLORS[2])
    polys = GLYPHS.get(key, GENERIC)
    c, r = SIZE / 2, SIZE / 2 - 2
    rows = []
    for py in range(SIZE):
        row = []
        for px in range(SIZE):
            # 4x supersampling for smooth edges
            cover = glyph = 0
            for sy in (.125, .375, .625, .875):
                for sx in (.125, .375, .625, .875):
                    x, y = px + sx, py + sy
                    if math.hypot(x - c, y - c) <= r:
                        cover += 1
                        u, v = (x - c) / (2 * r) * 1.3 + .5, (y - c) / (2 * r) * 1.3 + .5
                        if any(inside_polygon(u, v, p) for p in polys):
                            glyph += 1
            if cover == 0:
                row.append((0, 0, 0, 0))
                continue
            t = py / SIZE
            bg = [round(s + (e_ - s) * t) for s, e_ in zip(start, end)]
            g = glyph / cover
            rgb = [round(b + (255 - b) * g) for b in bg]
            row.append((*rgb, round(255 * cover / 16)))
        rows.append(row)
    return png(rows)


def main():
    root = sys.argv[1]
    if not os.path.exists(os.path.join(root, "description.xml")):
        root = os.path.join(root, "irma-demo")
    issuer_dir = os.path.join(root, ISSUER)
    os.makedirs(issuer_dir, exist_ok=True)
    logos = "--logos" in sys.argv
    with open(os.path.join(issuer_dir, "description.xml"), "w") as f:
        f.write(issuer_xml())
    if logos:
        with open(os.path.join(issuer_dir, "logo.png"), "wb") as f:
            f.write(logo(ISSUER))
    credentials = load_credentials()
    issues = os.path.join(issuer_dir, "Issues")
    if os.path.isdir(issues):
        for old in sorted(os.listdir(issues)):
            if old not in credentials and os.path.exists(os.path.join(issues, old, "description.xml")):
                shutil.rmtree(os.path.join(issues, old))
                print("removed", old)
    for cid, (name, desc, intro, singleton, attrs) in credentials.items():
        d = os.path.join(issuer_dir, "Issues", cid)
        os.makedirs(d, exist_ok=True)
        with open(os.path.join(d, "description.xml"), "w") as f:
            f.write(credential_xml(cid, name, desc, intro, singleton, attrs))
        if logos:
            with open(os.path.join(d, "logo.png"), "wb") as f:
                f.write(logo(cid))
    print("wrote", issuer_dir)


if __name__ == "__main__":
    main()
