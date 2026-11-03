"""Generates the pbdf-staging.pbdf credential definitions.

Usage: python3 scheme/generate.py <scheme-root>

Writes <scheme-root>/pbdf-staging/pbdf/Issues/<key>/description.xml (or
<scheme-root>/pbdf/Issues/... when <scheme-root> is itself the pbdf-staging
scheme, i.e. contains description.xml). The credentials and attributes come
from backend/credentials/definitions.json, the same file the backend uses.

The pbdf issuer is shared with other credentials, so the issuer description,
the logos and credentials that are no longer defined are left alone.
"""
import json
import os
import sys
from xml.sax.saxutils import escape as e

DEFINITIONS = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "backend", "credentials", "definitions.json")

SCHEME = "pbdf-staging"
ISSUER = "pbdf"
CATEGORY = ("Finance", "Financieel")
COLORS = ("#ffffff", "#0B4F3C", "#167A5B")
# where the Yivi app sends users to obtain a credential, and the credentials
# that are listed in the app's credential store
ISSUE_URL = "https://psd2-issuer.staging.yivi.app"
IN_STORE = {"bankaccount"}

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
    """Returns {key: (name, description, faq intro, singleton, attributes)}.

    Attributes are (id, name en, name nl, description en, description nl,
    display hint)."""
    with open(DEFINITIONS) as f:
        defs = json.load(f)
    period = [(a["id"], *pair(a["name"]), *pair(a["description"]), None) for a in defs["period_attributes"]]
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
            hint = None
            if a.get("e1"):
                aen, anl = f"E1 category {a['e1']}: {aen}{AMOUNT[0]}", f"E1-categorie {a['e1']}: {anl}{AMOUNT[1]}"
                hint = "currency:EUR"
            attrs.append((a["id"], *pair(a["name"]), aen, anl, hint))
        out[c["key"]] = (
            (en, nl),
            (den, dnl),
            (f"Add {lower_first(den)}, to your Yivi app.", f"Voeg {lower_first(dnl)}, toe aan je Yivi-app."),
            e1,
            attrs + (period if e1 else []),
        )
    return out


def tr(tag, pair, indent="\t"):
    return f"{indent}<{tag}>\n{indent}\t<en>{e(pair[0])}</en>\n{indent}\t<nl>{e(pair[1])}</nl>\n{indent}</{tag}>\n"


def credential_xml(cid, name, desc, intro, singleton, attrs):
    content = ("You will add the following data:\\n" + ",\\n".join("• " + lower_first(a[1]) for a in attrs) + ".",
               "Je voegt de volgende gegevens toe:\\n" + ",\\n".join("• " + lower_first(a[2]) for a in attrs) + ".")
    out = "<IssueSpecification version=\"4\">\n" + tr("Name", name) + tr("ShortName", name)
    out += f"\t<SchemeManager>{SCHEME}</SchemeManager>\n\t<IssuerID>{ISSUER}</IssuerID>\n\t<CredentialID>{cid}</CredentialID>\n"
    out += tr("Description", desc)
    if cid in IN_STORE:
        out += tr("IssueURL", (ISSUE_URL + "/en", ISSUE_URL + "/nl"))
        out += "\n\t<IsInCredentialStore>true</IsInCredentialStore>\n"
    if singleton:
        out += "\t<ShouldBeSingleton>true</ShouldBeSingleton>\n"
    out += (f"\n\t<ForegroundColor>{COLORS[0]}</ForegroundColor>\n"
            f"\t<BackgroundGradientStart>{COLORS[1]}</BackgroundGradientStart>\n"
            f"\t<BackgroundGradientEnd>{COLORS[2]}</BackgroundGradientEnd>\n\n")
    out += tr("Category", CATEGORY) + tr("FAQIntro", intro) + tr("FAQPurpose", PURPOSE)
    out += tr("FAQContent", content) + tr("FAQHowto", HOWTO)
    out += "\n\t<Attributes>\n"
    for i, en, nl, den, dnl, hint in attrs:
        hint = f" displayHint=\"{hint}\"" if hint else ""
        out += (f"\t\t<Attribute id=\"{i}\"{hint}>\n" + tr("Name", (en, nl), "\t\t\t") +
                tr("Description", (den, dnl), "\t\t\t") + "\t\t</Attribute>\n")
    out += "\t</Attributes>\n</IssueSpecification>\n"
    return out


def main():
    root = sys.argv[1]
    if not os.path.exists(os.path.join(root, "description.xml")):
        root = os.path.join(root, SCHEME)
    issues = os.path.join(root, ISSUER, "Issues")
    for cid, (name, desc, intro, singleton, attrs) in load_credentials().items():
        d = os.path.join(issues, cid)
        os.makedirs(d, exist_ok=True)
        with open(os.path.join(d, "description.xml"), "w") as f:
            f.write(credential_xml(cid, name, desc, intro, singleton, attrs))
    print("wrote", issues)


if __name__ == "__main__":
    main()
