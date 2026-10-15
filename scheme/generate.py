"""Generates the irma-demo.enablenow issuer and credential definitions.

Usage: python3 scheme/generate.py <scheme-root> [--logos]

Writes <scheme-root>/irma-demo/enablenow/... (or <scheme-root>/enablenow/...
when <scheme-root> is itself the irma-demo scheme, i.e. contains
description.xml). The attribute ids must match backend/credentials; the
backend test scheme_test.go checks this against the copy in scheme/.
"""
import math
import os
import struct
import sys
import zlib
from xml.sax.saxutils import escape as e

ISSUER = "enablenow"
CATEGORY = ("Finance", "Financieel")
COLORS = ("#ffffff", "#0B4F3C", "#167A5B")

M = ("Average per month in whole euros over the analysed period",
     "Gemiddelde per maand in hele euro's over de geanalyseerde periode")


def amt(i, en, nl):
    return (i, en, nl) + M


def yn(i, en, nl, den, dnl):
    return (i, en, nl, den + " ('yes' or 'no')", dnl + " ('yes' of 'no')")


PERIOD = [
    ("months", "Months analysed", "Geanalyseerde maanden",
     "Number of full calendar months the averages are based on",
     "Aantal volledige kalendermaanden waarop de gemiddelden zijn gebaseerd"),
    ("periodstart", "Period from", "Periode vanaf",
     "First day of the analysed period (YYYY-MM-DD)",
     "Eerste dag van de geanalyseerde periode (JJJJ-MM-DD)"),
    ("periodend", "Period until", "Periode tot en met",
     "Last day of the analysed period (YYYY-MM-DD)",
     "Laatste dag van de geanalyseerde periode (JJJJ-MM-DD)"),
]

HOWTO = ("1. Log in to your bank through enablenow and give permission to read your transactions.\\n"
         "2. Your transactions of the last 12 months are categorized automatically.\\n"
         "3. Check the results and choose which cards to add to your Yivi app.",
         "1. Log in bij je bank via enablenow en geef toestemming om je transacties te lezen.\\n"
         "2. Je transacties van de afgelopen 12 maanden worden automatisch gecategoriseerd.\\n"
         "3. Controleer de resultaten en kies welke kaartjes je aan je Yivi-app toevoegt.")

PURPOSE = ("With financial data in your Yivi app you can prove facts about your finances, for example to a landlord or lender, without handing over your bank statements.",
           "Met financiële gegevens in je Yivi-app kun je feiten over je financiën aantonen, bijvoorbeeld aan een verhuurder of geldverstrekker, zonder je bankafschriften af te geven.")

# key: (name, description, faq intro, singleton, attributes)
CREDENTIALS = {
    "income": (
        ("Demo Income", "Demo Inkomen"),
        ("Your average monthly income, derived from your bank transactions",
         "Je gemiddelde maandelijkse inkomen, afgeleid uit je banktransacties"),
        ("Add your average monthly income, derived from your bank transactions, to your Yivi app.",
         "Voeg je gemiddelde maandelijkse inkomen, afgeleid uit je banktransacties, toe aan je Yivi-app."),
        True, [
            amt("salary", "Salary per month", "Salaris per maand"),
            amt("pension", "Pension per month", "Pensioen per maand"),
            amt("otherincome", "Other income per month", "Overig inkomen per maand"),
            amt("total", "Total income per month", "Totaal inkomen per maand"),
            ("salarymonths", "Months with salary", "Maanden met salaris",
             "Number of analysed months in which salary was received",
             "Aantal geanalyseerde maanden waarin salaris is ontvangen"),
        ] + PERIOD),
    "housing": (
        ("Demo Housing and fixed costs", "Demo Wonen en vaste lasten"),
        ("Your average monthly housing and fixed costs, derived from your bank transactions",
         "Je gemiddelde maandelijkse woonlasten en vaste lasten, afgeleid uit je banktransacties"),
        ("Add your average monthly housing and fixed costs, derived from your bank transactions, to your Yivi app.",
         "Voeg je gemiddelde maandelijkse woonlasten en vaste lasten, afgeleid uit je banktransacties, toe aan je Yivi-app."),
        True, [
            ("housingtype", "Housing", "Woonsituatie",
             "'rent', 'mortgage', 'other' or 'none', based on the largest housing cost",
             "'rent', 'mortgage', 'other' of 'none', op basis van de grootste woonlast"),
            amt("housingcost", "Housing costs per month", "Woonlasten per maand"),
            amt("energy", "Energy per month", "Energie per maand"),
            amt("water", "Water and other utilities per month", "Water en overige nutsvoorzieningen per maand"),
            amt("insurance", "Insurance per month", "Verzekeringen per maand"),
            amt("telecom", "Telecom per month", "Telecom per maand"),
            amt("municipaltaxes", "Municipal taxes per month", "Gemeentelijke belastingen per maand"),
            amt("total", "Total fixed costs per month", "Totaal vaste lasten per maand"),
        ] + PERIOD),
    "debts": (
        ("Demo Debts", "Demo Schulden"),
        ("Your loan repayments and debt related signals, derived from your bank transactions",
         "Je aflossingen en schuldsignalen, afgeleid uit je banktransacties"),
        ("Add your loan repayments and debt related signals, derived from your bank transactions, to your Yivi app.",
         "Voeg je aflossingen en schuldsignalen, afgeleid uit je banktransacties, toe aan je Yivi-app."),
        True, [
            amt("loanrepayments", "Loan repayments per month", "Aflossingen per maand"),
            amt("studentloan", "Student loan repayments per month", "Aflossing studieschuld per maand"),
            yn("debtcollection", "Debt collection", "Incasso of deurwaarder",
               "Whether payments to a debt collector or bailiff were found",
               "Of er betalingen aan een incassobureau of deurwaarder zijn gevonden"),
            yn("flashloan", "Flash loans", "Flitskredieten",
               "Whether flash loan transactions were found",
               "Of er transacties van flitskredieten zijn gevonden"),
            yn("bnpl", "Buy now, pay later", "Achteraf betalen",
               "Whether buy now, pay later transactions were found",
               "Of er transacties van achteraf betalen zijn gevonden"),
            yn("gambling", "Gambling", "Gokken",
               "Whether gambling transactions were found",
               "Of er goktransacties zijn gevonden"),
        ] + PERIOD),
    "benefits": (
        ("Demo Benefits", "Demo Toeslagen en uitkeringen"),
        ("The allowances and benefits you receive, derived from your bank transactions",
         "De toeslagen en uitkeringen die je ontvangt, afgeleid uit je banktransacties"),
        ("Add the allowances and benefits you receive, derived from your bank transactions, to your Yivi app.",
         "Voeg de toeslagen en uitkeringen die je ontvangt, afgeleid uit je banktransacties, toe aan je Yivi-app."),
        True, [
            yn("receivesbenefits", "Receives benefits", "Ontvangt toeslagen of uitkeringen",
               "Whether any allowance or benefit was received",
               "Of er een toeslag of uitkering is ontvangen"),
            amt("healthcare", "Healthcare allowance per month", "Zorgtoeslag per maand"),
            amt("rent", "Rent allowance per month", "Huurtoeslag per maand"),
            amt("child", "Child benefits per month", "Kinderbijslag en kindgebonden budget per maand"),
            amt("unemployment", "Unemployment benefits per month", "WW-uitkering per maand"),
            amt("social", "Social assistance per month", "Bijstand per maand"),
            amt("disability", "Disability benefits per month", "Arbeidsongeschiktheidsuitkering per maand"),
            amt("student", "Student finance per month", "Studiefinanciering per maand"),
            amt("other", "Other benefits per month", "Overige toeslagen per maand"),
            amt("total", "Total benefits per month", "Totaal toeslagen en uitkeringen per maand"),
        ] + PERIOD),
    "bankaccount": (
        ("Demo Bank account", "Demo Bankrekening"),
        ("A bank account you have access to, verified through PSD2",
         "Een bankrekening waartoe je toegang hebt, geverifieerd via PSD2"),
        ("Add a bank account you have access to, verified through PSD2, to your Yivi app.",
         "Voeg een bankrekening waartoe je toegang hebt, geverifieerd via PSD2, toe aan je Yivi-app."),
        False, [
            ("iban", "IBAN", "IBAN", "International bank account number", "Internationaal bankrekeningnummer"),
            ("accountholder", "Account holder", "Rekeninghouder",
             "Name of the account holder as known by the bank",
             "Naam van de rekeninghouder zoals bekend bij de bank"),
            ("bank", "Bank", "Bank", "Identifier of the bank", "Identificatie van de bank"),
        ]),
}


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
    content = ("You will add the following data:\\n" + ",\\n".join("• " + a[1][0].lower() + a[1][1:] for a in attrs) + ".",
               "Je voegt de volgende gegevens toe:\\n" + ",\\n".join("• " + a[2][0].lower() + a[2][1:] for a in attrs) + ".")
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
    "debts": [[(.5, .82), (.80, .50), (.62, .50), (.62, .18), (.38, .18), (.38, .50), (.20, .50)]],
    "benefits": [[(.40, .18), (.60, .18), (.60, .40), (.82, .40), (.82, .60), (.60, .60), (.60, .82),
                  (.40, .82), (.40, .60), (.18, .60), (.18, .40), (.40, .40)]],
    "bankaccount": [[(.5, .16), (.86, .36), (.14, .36)], [(.20, .40), (.30, .40), (.30, .70), (.20, .70)],
                    [(.45, .40), (.55, .40), (.55, .70), (.45, .70)], [(.70, .40), (.80, .40), (.80, .70), (.70, .70)],
                    [(.14, .74), (.86, .74), (.86, .84), (.14, .84)]],
}
GLYPHS[ISSUER] = GLYPHS["bankaccount"]


def logo(key):
    start, end = hexrgb(COLORS[1]), hexrgb(COLORS[2])
    polys = GLYPHS[key]
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
    for cid, (name, desc, intro, singleton, attrs) in CREDENTIALS.items():
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
