package card

import (
	"bytes"
	"crypto/x509"
	"encoding/asn1"
	"encoding/hex"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	oidSignedData = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	oidData       = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	oidSHA256     = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidSHA384     = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 2}
	oidSHA512     = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 3}
	oidRSA        = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 1}
)

const pdfSigPlaceholderLen = 8192

func (ss *SignService) SignPDF(pdfBytes []byte, pin string, algorithm string) ([]byte, error) {
	if algorithm == "" {
		algorithm = AlgSHA256WithRSA
	}

	certDER, err := ss.GetSigningCertificate()
	if err != nil {
		return nil, fmt.Errorf("certificate error: %w", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}

	xrefOff, err := findXRefOffset(pdfBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid PDF: %w", err)
	}

	rootObjNum, err := findTrailerRoot(pdfBytes, xrefOff)
	if err != nil {
		return nil, err
	}

	catalogContent, err := readObjContent(pdfBytes, rootObjNum)
	if err != nil {
		return nil, fmt.Errorf("failed to read catalog: %w", err)
	}

	lastPageObj, err := findLastPage(pdfBytes, catalogContent)
	if err != nil {
		return nil, fmt.Errorf("find last page: %w", err)
	}

	mediaBox, err := parseMediaBox(pdfBytes, lastPageObj)
	if err != nil {
		mediaBox = []float64{0, 0, 612, 792}
	}

	maxObj := findMaxObjNum(pdfBytes)
	sigObj := maxObj + 1
	apObj := maxObj + 2
	fieldObj := maxObj + 3

	pdfBody := pdfBytes[:xrefOff]
	origObjs, pdfHeader := parseAllObjects(pdfBody)

	for i, o := range origObjs {
		if o.num == lastPageObj {
			origObjs[i].data = patchPageAnnots(o.data, fieldObj)
			break
		}
	}

	for i, o := range origObjs {
		if o.num == rootObjNum {
			origObjs[i].data = injectAcroForm(o.data, fieldObj)
			break
		}
	}

	now := time.Now().UTC().Format("20060102150405+00'00'")
	sigPH := strings.Repeat("0", pdfSigPlaceholderLen)
	signerCN := sanitizeName(cert.Subject.CommonName)
	issueDate := time.Now().UTC().Format("2006-01-02")

	sigDict := []byte(fmt.Sprintf(
		"<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached\n"+
			"   /ByteRange [0000000000 0000000000 0000000000 0000000000]\n"+
			"   /Contents <%s>\n"+
			"   /M (D:%s)\n>>", sigPH, now))

	apDict := []byte(buildSigAppearance("Digitally signed by", signerCN, issueDate, mediaBox))

	rect := sigRect(mediaBox)
	fieldDict := []byte(fmt.Sprintf(
		"<< /Type /Annot /Subtype /Widget /FT /Sig\n"+
			"   /Rect [%.1f %.1f %.1f %.1f] /V %d 0 R /T (Signature1)\n"+
			"   /F 4 /P %d 0 R /AP << /N %d 0 R >>\n>>",
		rect[0], rect[1], rect[2], rect[3], sigObj, lastPageObj, apObj))

	finalObjs := make([]objEntry, 0, len(origObjs)+3)
	for _, o := range origObjs {
		finalObjs = append(finalObjs, objEntry{o.num, o.data})
	}
	finalObjs = append(finalObjs, objEntry{sigObj, sigDict}, objEntry{apObj, apDict}, objEntry{fieldObj, fieldDict})

	sort.Slice(finalObjs, func(i, j int) bool {
		return finalObjs[i].num < finalObjs[j].num
	})

	var body bytes.Buffer
	offsets := make(map[int]int64)
	body.Write(pdfHeader)

	for _, o := range finalObjs {
		offsets[o.num] = int64(body.Len())
		fmt.Fprintf(&body, "%d 0 obj\n", o.num)
		body.Write(o.data)
		if !bytes.HasSuffix(o.data, []byte("endobj")) {
			fmt.Fprintf(&body, "\nendobj\n")
		} else {
			body.WriteByte('\n')
		}
	}

	bodyBytes := body.Bytes()
	brMarker := []byte("/ByteRange [")
	brIdx := bytes.Index(bodyBytes, brMarker)
	if brIdx < 0 {
		return nil, fmt.Errorf("ByteRange marker not found in assembled body")
	}
	brNumStart := int64(brIdx) + int64(len(brMarker))

	contentsMarker := []byte("/Contents <")
	contentsIdx := bytes.Index(bodyBytes, contentsMarker)
	if contentsIdx < 0 {
		return nil, fmt.Errorf("Contents marker not found in assembled body")
	}
	hexStart := int64(contentsIdx) + int64(len(contentsMarker))
	hexEnd := hexStart + pdfSigPlaceholderLen

	var output bytes.Buffer
	output.Write(body.Bytes())
	xrefPos := int64(output.Len())

	maxObjNum := 0
	for _, o := range finalObjs {
		if o.num > maxObjNum {
			maxObjNum = o.num
		}
	}

	var trailer bytes.Buffer
	fmt.Fprintf(&trailer, "xref\n")
	fmt.Fprintf(&trailer, "0 %d\n", maxObjNum+1)
	fmt.Fprintf(&trailer, "0000000000 65535 f \n")
	for i := 1; i <= maxObjNum; i++ {
		off, exists := offsets[i]
		if exists {
			fmt.Fprintf(&trailer, "%010d 00000 n \n", off)
		} else {
			fmt.Fprintf(&trailer, "0000000000 00000 f \n")
		}
	}
	fmt.Fprintf(&trailer, "trailer\n")
	fmt.Fprintf(&trailer, "<< /Size %d /Root %d 0 R /Prev %d >>\n", maxObjNum+1, rootObjNum, xrefOff)
	fmt.Fprintf(&trailer, "startxref\n")
	fmt.Fprintf(&trailer, "%d\n", xrefPos)
	fmt.Fprintf(&trailer, "%%%%EOF\n")
	output.Write(trailer.Bytes())

	totalLen := int64(output.Len())
	result := output.Bytes()

	brValues := []int64{0, hexStart, hexEnd, totalLen}
	for i, val := range brValues {
		numStr := fmt.Sprintf("%010d", val)
		copy(result[brNumStart+int64(i*11):], numStr)
	}

	hashData := make([]byte, 0, hexStart+(totalLen-hexEnd))
	hashData = append(hashData, result[:hexStart]...)
	hashData = append(hashData, result[hexEnd:]...)

	rawSig, err := ss.Sign(hashData, pin, algorithm)
	if err != nil {
		return nil, fmt.Errorf("signing failed: %w", err)
	}

	cmsBytes, err := buildCMSSignature(rawSig, certDER, algorithm)
	if err != nil {
		return nil, fmt.Errorf("CMS build failed: %w", err)
	}

	sigHex := hex.EncodeToString(cmsBytes)
	if len(sigHex) > pdfSigPlaceholderLen {
		return nil, fmt.Errorf("CMS signature too large (%d hex chars, max %d)", len(sigHex), pdfSigPlaceholderLen)
	}
	sigHexPadded := sigHex + strings.Repeat("0", pdfSigPlaceholderLen-len(sigHex))
	copy(result[hexStart:hexEnd], sigHexPadded)

	return result, nil
}

// ── PDF structure parsing ──

func parseAllObjects(pdfBody []byte) ([]objEntry, []byte) {
	re := regexp.MustCompile(`(\d+)\s+0\s+obj\s*\n?`)
	matches := re.FindAllSubmatchIndex(pdfBody, -1)
	if len(matches) == 0 {
		return nil, pdfBody
	}
	header := pdfBody[:matches[0][0]]
	var objs []objEntry
	for _, loc := range matches {
		num, _ := strconv.Atoi(string(pdfBody[loc[2]:loc[3]]))
		contentStart := loc[1]
		for contentStart < len(pdfBody) && (pdfBody[contentStart] == '\n' || pdfBody[contentStart] == '\r' || pdfBody[contentStart] == ' ') {
			contentStart++
		}
		endIdx := bytes.Index(pdfBody[contentStart:], []byte("endobj"))
		if endIdx < 0 {
			continue
		}
		content := pdfBody[contentStart : contentStart+endIdx]
		objs = append(objs, objEntry{num: num, data: content})
	}
	return objs, header
}

type objEntry struct {
	num  int
	data []byte
}

func findXRefOffset(pdf []byte) (int64, error) {
	idx := bytes.LastIndex(pdf, []byte("startxref"))
	if idx < 0 {
		return 0, fmt.Errorf("startxref not found")
	}
	rest := pdf[idx+9:]
	i := 0
	for i < len(rest) && (rest[i] == '\n' || rest[i] == '\r' || rest[i] == ' ') {
		i++
	}
	j := i
	for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
		j++
	}
	if j == i {
		return 0, fmt.Errorf("no xref offset")
	}
	return strconv.ParseInt(string(rest[i:j]), 10, 64)
}

func findTrailerRoot(pdf []byte, xrefOff int64) (int, error) {
	region := pdf[xrefOff:]
	trailerIdx := bytes.Index(region, []byte("trailer"))
	if trailerIdx < 0 {
		return 0, fmt.Errorf("trailer not found")
	}
	dict := string(region[trailerIdx:])
	endIdx := strings.Index(dict, ">>")
	if endIdx < 0 {
		return 0, fmt.Errorf("trailer >> not found")
	}
	dict = dict[:endIdx+2]
	re := regexp.MustCompile(`/Root\s+(\d+)\s+0\s+R`)
	matches := re.FindStringSubmatch(dict)
	if matches == nil {
		return 0, fmt.Errorf("/Root not found in trailer")
	}
	n, _ := strconv.Atoi(matches[1])
	return n, nil
}

func readObjContent(pdf []byte, objNum int) ([]byte, error) {
	pattern := fmt.Sprintf("%d 0 obj", objNum)
	idx := bytes.Index(pdf, []byte(pattern))
	if idx < 0 {
		return nil, fmt.Errorf("object %d not found", objNum)
	}
	start := idx + len(pattern)
	for start < len(pdf) && (pdf[start] == '\n' || pdf[start] == '\r' || pdf[start] == ' ') {
		start++
	}
	endIdx := bytes.Index(pdf[start:], []byte("endobj"))
	if endIdx < 0 {
		return nil, fmt.Errorf("endobj not found for object %d", objNum)
	}
	return bytes.TrimSpace(pdf[start : start+endIdx]), nil
}

func findMaxObjNum(pdf []byte) int {
	re := regexp.MustCompile(`(\d+)\s+0\s+obj`)
	matches := re.FindAllSubmatch(pdf, -1)
	max := 0
	for _, m := range matches {
		n, _ := strconv.Atoi(string(m[1]))
		if n > max {
			max = n
		}
	}
	return max
}

func escapePDFText(s string) string {
	var buf bytes.Buffer
	for _, r := range s {
		for _, c := range transliterateToASCII(r) {
			if c == '\\' || c == '(' || c == ')' {
				buf.WriteByte('\\')
			}
			buf.WriteRune(c)
		}
	}
	return buf.String()
}

// sanitizeName strips non-printable and invisible Unicode characters from a name.
func sanitizeName(name string) string {
	var buf bytes.Buffer
	for _, r := range name {
		if isInvisibleOrFormat(r) {
			continue
		}
		buf.WriteRune(r)
	}
	return strings.TrimSpace(buf.String())
}

// isInvisibleOrFormat returns true for Unicode characters that are invisible,
// formatting, or otherwise not renderable in a PDF standard font.
func isInvisibleOrFormat(r rune) bool {
	// Control characters (except tab, newline, CR)
	if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
		return true
	}
	// DEL
	if r == 0x7F {
		return true
	}
	// C1 control characters
	if r >= 0x80 && r <= 0x9F {
		return true
	}
	// Unicode Format characters (Cf category) — covers all invisible/formatting code points
	switch {
	case r == 0x00AD: // SOFT HYPHEN
		return true
	case r >= 0x200B && r <= 0x200F: // ZWSP, ZWNJ, ZWJ, LRM, RLM, LRO
		return true
	case r >= 0x2028 && r <= 0x202E: // Line/paragraph separators, bidi
		return true
	case r >= 0x2060 && r <= 0x2064: // Word joiner, invisible operators
		return true
	case r >= 0x2066 && r <= 0x2069: // Bidi isolates
		return true
	case r == 0xFEFF: // BOM / ZWNBSP
		return true
	case r >= 0xFFF9 && r <= 0xFFFB: // Interlinear annotations
		return true
	case r >= 0x1D173 && r <= 0x1D17A: // Musical symbols
		return true
	case r == 0x2800: // Braille pattern blank
		return true
	}
	return false
}

// transliterateToASCII converts Unicode characters to ASCII equivalents.
// Handles Cyrillic (Serbian), extended Latin (diacritics), and other common characters.
func transliterateToASCII(r rune) string {
	// Serbian Cyrillic using Unicode code points (U+0400-U+04FF block)
	switch r {
	case '\u0410':
		return "A" // А
	case '\u0430':
		return "a" // а
	case '\u0411':
		return "B" // Б
	case '\u0431':
		return "b" // б
	case '\u0412':
		return "V" // В
	case '\u0432':
		return "v" // в
	case '\u0413':
		return "G" // Г
	case '\u0433':
		return "g" // г
	case '\u0414':
		return "D" // Д
	case '\u0434':
		return "d" // д
	case '\u0402':
		return "Dj" // Ђ
	case '\u0452':
		return "dj" // ђ
	case '\u0415':
		return "E" // Е
	case '\u0435':
		return "e" // е
	case '\u0416':
		return "Zh" // Ж
	case '\u0436':
		return "zh" // ж
	case '\u0417':
		return "Z" // З
	case '\u0437':
		return "z" // з
	case '\u0418':
		return "I" // И
	case '\u0438':
		return "i" // и
	case '\u0408':
		return "J" // Ј
	case '\u0458':
		return "j" // ј
	case '\u041A':
		return "K" // К
	case '\u043A':
		return "k" // к
	case '\u041B':
		return "L" // Л
	case '\u043B':
		return "l" // л
	case '\u0409':
		return "Lj" // Љ
	case '\u0459':
		return "lj" // љ
	case '\u041C':
		return "M" // М
	case '\u043C':
		return "m" // м
	case '\u041D':
		return "N" // Н
	case '\u043D':
		return "n" // н
	case '\u040A':
		return "Nj" // Њ
	case '\u045A':
		return "nj" // њ
	case '\u041E':
		return "O" // О
	case '\u043E':
		return "o" // о
	case '\u041F':
		return "P" // П
	case '\u043F':
		return "p" // п
	case '\u0420':
		return "R" // Р
	case '\u0440':
		return "r" // р
	case '\u0421':
		return "S" // С
	case '\u0441':
		return "s" // с
	case '\u0422':
		return "T" // Т
	case '\u0442':
		return "t" // т
	case '\u040B':
		return "C" // Ћ
	case '\u045B':
		return "c" // ћ
	case '\u0423':
		return "U" // У
	case '\u0443':
		return "u" // у
	case '\u0424':
		return "F" // Ф
	case '\u0444':
		return "f" // ф
	case '\u0425':
		return "H" // Х
	case '\u0445':
		return "h" // х
	case '\u0426':
		return "C" // Ц
	case '\u0446':
		return "c" // ц
	case '\u0427':
		return "Ch" // Ч
	case '\u0447':
		return "ch" // ч
	case '\u042F':
		return "Ya" // Я
	case '\u044F':
		return "ya" // я
	case '\u042C':
		return "" // Ь (soft sign)
	case '\u044C':
		return "" // ь (soft sign)
	case '\u042B':
		return "Y" // Ы
	case '\u044B':
		return "y" // ы
	case '\u042A':
		return "" // Ъ (hard sign)
	case '\u044A':
		return "" // ъ (hard sign)
	case '\u0429':
		return "Shch" // Щ
	case '\u0449':
		return "shch" // щ
	case '\u0428':
		return "Sh" // Ш
	case '\u0448':
		return "sh" // ш
	case '\u0406':
		return "I" // І
	case '\u0456':
		return "i" // і
	case '\u0407':
		return "Yi" // Ї
	case '\u0457':
		return "yi" // ї
	case '\u0404':
		return "Ye" // Є
	case '\u0454':
		return "ye" // є
	case '\u0490':
		return "G" // Ґ
	case '\u0491':
		return "g" // ґ
	case '\u0450':
		return "e" // ѐ
	case '\u0400':
		return "E" // Ѐ
	case '\u040D':
		return "I" // Ѝ
	case '\u045D':
		return "i" // ѝ
	case '\u040E':
		return "Yu" // Ў
	case '\u045E':
		return "yu" // ў
	case '\u0401':
		return "Yo" // Ё
	case '\u0451':
		return "yo" // ё
	case '\u04D9':
		return "a" // ә
	case '\u04D8':
		return "A" // Ә
	}

	// Extended Latin
	switch r {
	case '\u0106', '\u0107':
		return "C" // Ć, ć
	case '\u010C', '\u010D':
		return "C" // Č, č
	case '\u0160', '\u0161':
		return "S" // Š, š
	case '\u017D', '\u017E':
		return "Z" // Ž, ž
	case '\u0110', '\u0111':
		return "DJ" // Đ, đ
	case '\u00D6':
		return "O" // Ö
	case '\u00DC':
		return "U" // Ü
	case '\u00E4':
		return "ae" // ä
	case '\u00F6':
		return "o" // ö
	case '\u00FC':
		return "u" // ü
	case '\u00DF':
		return "ss" // ß
	case '\u00C9', '\u00E9':
		return "E" // É, é
	case '\u00C0', '\u00E0':
		return "A" // À, à
	case '\u00D1', '\u00F1':
		return "N" // Ñ, ñ
	}

	// Fallback - pass through printable ASCII, drop unknown characters silently
	if r >= 0x20 && r <= 0x7E {
		return string(r)
	}
	return ""

}

// ── Page navigation ──

func patchPageAnnots(pageContent []byte, widgetObjNum int) []byte {
	s := string(pageContent)
	widgetRef := fmt.Sprintf("%d 0 R", widgetObjNum)
	annotsRe := regexp.MustCompile(`/Annots\s*\[([^\]]*)\]`)
	m := annotsRe.FindStringSubmatch(s)
	if m != nil {
		existing := strings.TrimSpace(m[1])
		if existing == "" {
			newAnnots := fmt.Sprintf("/Annots [%s]", widgetRef)
			return []byte(strings.Replace(s, m[0], newAnnots, 1))
		}
		newAnnots := fmt.Sprintf("/Annots [%s %s]", existing, widgetRef)
		return []byte(strings.Replace(s, m[0], newAnnots, 1))
	}
	annotsEntry := fmt.Sprintf("\n   /Annots [%d 0 R]", widgetObjNum)
	lastClose := strings.LastIndex(s, ">>")
	if lastClose < 0 {
		return append(pageContent, []byte(annotsEntry+"\n>>")...)
	}
	result := make([]byte, 0, len(s)+len(annotsEntry))
	result = append(result, []byte(s[:lastClose])...)
	result = append(result, []byte(annotsEntry)...)
	result = append(result, []byte(s[lastClose:])...)
	return result
}

func findLastPage(pdf []byte, catalog []byte) (int, error) {
	re := regexp.MustCompile(`/Type\s*/Pages`)
	pagesRefRe := regexp.MustCompile(`/Kids\s*\[([^\]]+)\]`)
	pagesRefRe2 := regexp.MustCompile(`/Pages\s+(\d+)\s+0\s+R`)
	m := pagesRefRe2.FindStringSubmatch(string(catalog))
	if m == nil {
		return 0, fmt.Errorf("/Pages not found in catalog")
	}
	pagesObjNum, _ := strconv.Atoi(m[1])
	currentObjNum := pagesObjNum
	for {
		content, err := readObjContent(pdf, currentObjNum)
		if err != nil {
			return 0, err
		}
		s := string(content)
		if re.MatchString(s) {
			kidsMatch := pagesRefRe.FindStringSubmatch(s)
			if kidsMatch == nil {
				return 0, fmt.Errorf("no /Kids in Pages node %d", currentObjNum)
			}
			kids := kidsMatch[1]
			kidRefRe := regexp.MustCompile(`(\d+)\s+0\s+R`)
			kidRefs := kidRefRe.FindAllStringSubmatch(kids, -1)
			if len(kidRefs) == 0 {
				return 0, fmt.Errorf("no kid references")
			}
			lastKid, _ := strconv.Atoi(kidRefs[len(kidRefs)-1][1])
			currentObjNum = lastKid
			continue
		}
		return currentObjNum, nil
	}
}

func parseMediaBox(pdf []byte, pageObjNum int) ([]float64, error) {
	content, err := readObjContent(pdf, pageObjNum)
	if err != nil {
		return nil, err
	}
	re := regexp.MustCompile(`/MediaBox\s*\[([^\]]+)\]`)
	m := re.FindStringSubmatch(string(content))
	if m == nil {
		return nil, fmt.Errorf("no /MediaBox")
	}
	fields := strings.Fields(m[1])
	if len(fields) != 4 {
		return nil, fmt.Errorf("bad /MediaBox")
	}
	vals := make([]float64, 4)
	for i, f := range fields {
		v, err := strconv.ParseFloat(f, 64)
		if err != nil {
			return nil, err
		}
		vals[i] = v
	}
	return vals, nil
}

func sigRect(mediaBox []float64) [4]float64 {
	sigW := 220.0
	sigH := 60.0
	x1 := mediaBox[2] - sigW - 10
	y1 := mediaBox[1] + 10
	return [4]float64{x1, y1, x1 + sigW, y1 + sigH}
}

func buildSigAppearance(label, name, date string, mediaBox []float64) string {
	var cs bytes.Buffer
	w := 220.0
	h := 70.0

	// Light gray background with rounded border
	cs.WriteString("0.95 0.95 0.95 rg\n")
	fmt.Fprintf(&cs, "2 2 %.0f %.0f re f\n", w-4, h-4)
	cs.WriteString("0 0 0 rg\n")
	cs.WriteString("0.5 0.5 0.5 RG 0.5 w\n")
	fmt.Fprintf(&cs, "1 1 m %.0f 1 l %.0f %.0f l 1 %.0f l c S\n", w-1, w-1, h-1, h-1)

	// Signature label (small, gray)
	cs.WriteString("0.4 0.4 0.4 rg\n")
	cs.WriteString("BT /F1 8 Tf\n")
	fmt.Fprintf(&cs, "8 %.0f Td\n", h-16)
	cs.WriteString("(" + escapePDFText(label) + ") Tj ET\n")

	// Separator line
	cs.WriteString("0.6 0.6 0.6 RG 0.3 w\n")
	fmt.Fprintf(&cs, "8 %.0f m %.0f %.0f l S\n", h-22, w-8, h-22)

	// Signer name (bold, larger)
	cs.WriteString("0 0 0 rg\n")
	cs.WriteString("BT /F1 10 Tf\n")
	fmt.Fprintf(&cs, "8 %.0f Td\n", h-34)
	runes := []rune(name)
	maxNameChars := 28
	if len(runes) > maxNameChars {
		name = string(runes[:maxNameChars]) + "\u2026"
	}
	cs.WriteString("(" + escapePDFText(name) + ") Tj ET\n")

	// Date (small, gray)
	cs.WriteString("0.4 0.4 0.4 rg\n")
	cs.WriteString("BT /F1 7 Tf\n")
	fmt.Fprintf(&cs, "8 %.0f Td\n", h-48)
	cs.WriteString("(" + escapePDFText(date) + ") Tj ET\n")

	// Bottom accent line (dark blue)
	cs.WriteString("0.1 0.3 0.6 rg\n")
	fmt.Fprintf(&cs, "8 6 m %.0f 6 l S\n", w-8)

	streamBytes := cs.Bytes()
	var obj bytes.Buffer
	fmt.Fprintf(&obj, "<< /Type /XObject /Subtype /Form /BBox [0 0 %.0f %.0f]\n", w, h)
	fmt.Fprintf(&obj, "   /Resources << /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> >>\n")
	fmt.Fprintf(&obj, "   /Length %d\n>>\nstream\n", len(streamBytes))
	obj.Write(streamBytes)
	fmt.Fprintf(&obj, "\nendstream")
	return obj.String()
}

func injectAcroForm(catalog []byte, fieldObjNum int) []byte {
	s := string(catalog)
	acroForm := fmt.Sprintf("\n   /AcroForm << /Fields [%d 0 R] /SigFlags 3 >>", fieldObjNum)
	lastClose := strings.LastIndex(s, ">>")
	if lastClose < 0 {
		return []byte(s + acroForm + "\n>>")
	}
	result := s[:lastClose] + acroForm + "\n" + s[lastClose:]
	return []byte(result)
}

// ── CMS/PKCS#7 ──

func buildCMSSignature(signature []byte, certDER []byte, algorithm string) ([]byte, error) {
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}
	hashOID := getHashOID(algorithm)
	algID := derSeq(derOID(hashOID), derNull())
	digestAlgSet := derSet(algID)
	encapInfo := derSeq(derOID(oidData))
	serialBytes, err := asn1.Marshal(cert.SerialNumber)
	if err != nil {
		return nil, fmt.Errorf("marshal serial: %w", err)
	}
	issuerAndSerial := derSeq(cert.RawIssuer, serialBytes)
	digestEncAlg := derSeq(derOID(oidRSA), derNull())
	signerInfo := derSeq(derInt(1), issuerAndSerial, algID, digestEncAlg, derOctetString(signature))
	certSet := derSet(certDER)
	certs := derImplicitConstructed(0, certSet)
	signedData := derSeq(derInt(1), digestAlgSet, encapInfo, certs, derSet(signerInfo))
	return derSeq(derOID(oidSignedData), derExplicit(0, signedData)), nil
}

func getHashOID(algorithm string) asn1.ObjectIdentifier {
	switch algorithm {
	case AlgSHA384WithRSA:
		return oidSHA384
	case AlgSHA512WithRSA:
		return oidSHA512
	default:
		return oidSHA256
	}
}

// ── DER encoding ──

func derInt(n int) []byte {
	b, _ := asn1.Marshal(big.NewInt(int64(n)))
	return b
}

func derOctetString(b []byte) []byte {
	return wrapTag(asn1.TagOctetString, b)
}

func derOID(oid asn1.ObjectIdentifier) []byte {
	b, _ := asn1.Marshal(oid)
	return b
}

func derNull() []byte {
	return []byte{0x05, 0x00}
}

func derSeq(components ...[]byte) []byte {
	return wrapTag(asn1.TagSequence|0x20, concat(components...))
}

func derSet(components ...[]byte) []byte {
	return wrapTag(asn1.TagSet|0x20, concat(components...))
}

func derExplicit(tagNum byte, content []byte) []byte {
	return wrapTag(byte(0xA0)|tagNum, content)
}

func derImplicitConstructed(tagNum byte, seqOrSet []byte) []byte {
	ctxTag := byte(0xA0) | tagNum
	if len(seqOrSet) == 0 {
		return wrapTag(ctxTag, nil)
	}
	result := make([]byte, len(seqOrSet))
	copy(result, seqOrSet)
	result[0] = ctxTag
	return result
}

func wrapTag(tag byte, content []byte) []byte {
	var buf bytes.Buffer
	buf.WriteByte(tag)
	writeLength(&buf, len(content))
	buf.Write(content)
	return buf.Bytes()
}

func writeLength(buf *bytes.Buffer, length int) {
	switch {
	case length < 0x80:
		buf.WriteByte(byte(length))
	case length < 0x100:
		buf.WriteByte(0x81)
		buf.WriteByte(byte(length))
	case length < 0x10000:
		buf.WriteByte(0x82)
		buf.WriteByte(byte(length >> 8))
		buf.WriteByte(byte(length))
	default:
		buf.WriteByte(0x83)
		buf.WriteByte(byte(length >> 16))
		buf.WriteByte(byte(length >> 8))
		buf.WriteByte(byte(length))
	}
}

func concat(parts ...[]byte) []byte {
	var buf bytes.Buffer
	for _, p := range parts {
		buf.Write(p)
	}
	return buf.Bytes()
}
