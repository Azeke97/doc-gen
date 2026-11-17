#!/usr/bin/env python3
"""Собирает examples/*/template.docx из template.html.

LibreOffice переводит HTML в DOCX, затем скрипт делает страницу альбомной A4
и добавляет нижний колонтитул «{{ number }} … стр. N / M» (метка в колонтитуле
проверяет, что движок ищет метки и там).

Запуск из корня репозитория: python3 examples/build.py
"""

import pathlib
import re
import shutil
import subprocess
import sys
import tempfile
import zipfile

ROOT = pathlib.Path(__file__).resolve().parent

W = 'xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"'
R = 'xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"'


def field(instr: str) -> str:
    return (
        '<w:r><w:fldChar w:fldCharType="begin"/></w:r>'
        f'<w:r><w:instrText xml:space="preserve"> {instr} </w:instrText></w:r>'
        '<w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>1</w:t></w:r>'
        '<w:r><w:fldChar w:fldCharType="end"/></w:r>'
    )


FOOTER = (
    f'<?xml version="1.0" encoding="UTF-8" standalone="yes"?>\n<w:ftr {W} {R}>'
    '<w:p><w:pPr><w:pBdr><w:top w:val="single" w:sz="6" w:space="1" w:color="000000"/></w:pBdr>'
    '<w:tabs><w:tab w:val="right" w:pos="15398"/></w:tabs></w:pPr>'
    '<w:r><w:rPr><w:b/><w:i/><w:sz w:val="16"/></w:rPr><w:t>{{ number }}</w:t></w:r>'
    '<w:r><w:rPr><w:sz w:val="16"/></w:rPr><w:tab/><w:t xml:space="preserve">стр. / бет </w:t></w:r>'
    + field("PAGE")
    + '<w:r><w:t xml:space="preserve"> / </w:t></w:r>'
    + field("NUMPAGES")
    + "</w:p></w:ftr>"
)

# Альбомная A4, поля 1.5 см.
SECT = (
    '<w:footerReference w:type="default" r:id="rIdFooterDG"/>'
    '<w:pgSz w:w="16838" w:h="11906" w:orient="landscape"/>'
    '<w:pgMar w:top="850" w:right="720" w:bottom="850" w:left="720" w:header="425" w:footer="425" w:gutter="0"/>'
)


NUMBER_COL = 1000  # ширина колонки «№», twips (≈1.8 см: влезает семизначный ID заявки)


def narrow_number_column(m: re.Match) -> str:
    """LibreOffice делает колонку «№» слишком широкой: отдаём лишнее второй колонке."""
    tbl = m.group(0)
    grid = re.search(r"<w:tblGrid>(.*?)</w:tblGrid>", tbl, re.S)
    widths = [int(w) for w in re.findall(r'<w:gridCol w:w="(\d+)"', grid.group(1))]
    if len(widths) < 3 or ">№<" not in tbl or widths[0] <= NUMBER_COL:
        return tbl
    delta = widths[0] - NUMBER_COL
    widths[0], widths[1] = NUMBER_COL, widths[1] + delta
    new_grid = "<w:tblGrid>" + "".join(f'<w:gridCol w:w="{w}"/>' for w in widths) + "</w:tblGrid>"
    tbl = tbl[: grid.start()] + new_grid + tbl[grid.end():]

    def fix_row(r: re.Match) -> str:
        row = r.group(0)
        cells = list(re.finditer(r"<w:tc>.*?</w:tc>", row, re.S))
        if len(cells) < 2 or "<w:gridSpan" in cells[0].group(0):
            return row
        out, pos = [], 0
        for c, w in zip(cells[:2], widths[:2]):
            out.append(row[pos:c.start()])
            out.append(re.sub(r'(<w:tcW w:w=")\d+', rf"\g<1>{w}", c.group(0), count=1))
            pos = c.end()
        return "".join(out) + row[pos:]

    return re.sub(r"<w:tr\b.*?</w:tr>", fix_row, tbl, flags=re.S)


def patch(docx: pathlib.Path) -> None:
    with zipfile.ZipFile(docx) as z:
        files = {n: z.read(n) for n in z.namelist()}

    doc = files["word/document.xml"].decode()
    if "xmlns:r=" not in doc[:2000]:
        doc = doc.replace("<w:document ", f"<w:document {R} ", 1)
    # Последний sectPr: убрать прежние размеры/колонтитулы и вставить свои.
    start = doc.rfind("<w:sectPr")
    end = doc.find("</w:sectPr>", start)
    sect = doc[start:end]
    sect = re.sub(r"<w:(footerReference|headerReference|pgSz|pgMar)\b[^>]*/>", "", sect)
    head_end = sect.find(">") + 1
    sect = sect[:head_end] + SECT + sect[head_end:]
    doc = doc[:start] + sect + doc[end:]
    doc = re.sub(r"<w:tbl>.*?</w:tbl>", narrow_number_column, doc, flags=re.S)
    files["word/document.xml"] = doc.encode()

    files["word/footer9.xml"] = FOOTER.encode()

    rels = files["word/_rels/document.xml.rels"].decode()
    rels = rels.replace(
        "</Relationships>",
        '<Relationship Id="rIdFooterDG" '
        'Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" '
        'Target="footer9.xml"/></Relationships>',
    )
    files["word/_rels/document.xml.rels"] = rels.encode()

    ct = files["[Content_Types].xml"].decode()
    ct = ct.replace(
        "</Types>",
        '<Override PartName="/word/footer9.xml" '
        'ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml"/></Types>',
    )
    files["[Content_Types].xml"] = ct.encode()

    tmp = docx.with_suffix(".tmp")
    with zipfile.ZipFile(tmp, "w", zipfile.ZIP_DEFLATED) as z:
        for name, data in files.items():
            z.writestr(name, data)
    tmp.replace(docx)


def build(html: pathlib.Path) -> None:
    with tempfile.TemporaryDirectory() as out:
        subprocess.run(
            ["soffice", "--headless", "--infilter=HTML (StarWriter)",
             "--convert-to", "docx:MS Word 2007 XML", "--outdir", out, str(html)],
            check=True, capture_output=True,
        )
        target = html.with_suffix(".docx")
        shutil.move(pathlib.Path(out) / (html.stem + ".docx"), target)
    patch(target)
    print(f"{target.relative_to(ROOT.parent)}")


def build_broken() -> None:
    """Шаблон с ошибкой для проверки 422: из протокола вскрытия убран первый {{/each}}."""
    src = ROOT / "opening" / "template.docx"
    target = ROOT / "broken" / "unclosed_each.docx"
    target.parent.mkdir(exist_ok=True)
    with zipfile.ZipFile(src) as z:
        files = {n: z.read(n) for n in z.namelist()}
    files["word/document.xml"] = files["word/document.xml"].replace(b"{{/each}}", b"", 1)
    with zipfile.ZipFile(target, "w", zipfile.ZIP_DEFLATED) as z:
        for name, data in files.items():
            z.writestr(name, data)
    print(f"{target.relative_to(ROOT.parent)}")


if __name__ == "__main__":
    for html in sorted(ROOT.glob("*/template.html")):
        build(html)
    build_broken()
    sys.exit(0)
