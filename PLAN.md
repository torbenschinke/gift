# Gift: Architektur- und Implementierungsplan

Status: Entwurf zum Review, Revision 2, 2026-09-13. Noch keine implementierte API.

Dieses Dokument beschreibt den ersten vertikalen Prototyp und seine Grenzen.
Codebeispiele sind API-Skizzen, keine bereits kompilierten Zusagen. Erst nach
Review beginnt die Implementierung.

Modulpfad: `github.com/torbenschinke/gift`. Toolchain: Go 1.27 (generische
Methoden), Ebitengine 2.10.1. Die go.mod ist bereits entsprechend gesetzt;
Ebitengine wird in Schritt 1 gepinnt.

## 1. Festgelegte Ziele

- SwiftUI-artige, deklarative Go-API nach Variante A: konkrete Value-Views und
  fluent Modifier, heterogene Kinderlisten ueber eine schmale View-Schnittstelle.
- Generics und generische Methoden dort, wo sie Typsicherheit oder konkrete
  Speicherung verbessern. Keine Reflection im regulaeren Layout-/Renderpfad.
- Rendererunabhaengige Deklaration, gemeinsames Layout und ein CGO-freies
  Ebitengine-Desktop-Backend.
- Zielplattform: Raspberry Pi 4 und Pi 5, Raspberry Pi OS 64-Bit,
  X11/XWayland, hardwarebeschleunigtes Mesa, 1920x1080 bei 60 Hz.
- Erster Aufschlag enthaelt VStack, HStack, Text, Button und eine virtuelle
  Bildergalerie mit 10.000 bis 100.000 Eintraegen.
- Border, abgerundete Formen und Shadow gehoeren zum ersten Aufschlag.
  Ein experimentelles glassartiges Material zeigt die Effektarchitektur.
- Image-Loading, Metadaten-Probing, Thumbnail-Erzeugung und Caching werden vom
  Framework gesteuert. Standardquellen: lokales Dateisystem und HTTP.
- Eingabe umfasst Maus, Tastatur und einfache Touch-Gesten (Tap, Drag-Scroll,
  kinetisches Scrollen). Siehe Abschnitt 6.
- Beispiele liegen unter cmd/example-*; Tests auf der Zielhardware uebernimmt
  der Nutzer. Lokal werden Korrektheit, Builds und Allokationsziele geprueft.

60 fps ist ein zu validierendes Ziel, keine bereits nachgewiesene Eigenschaft.
Insbesondere Glass-Effekte und kaltes Nachladen erhalten eigene Messszenarien.
Die Abnahmeschwellen stehen in Abschnitt 13.

## 2. Was wir von Nago uebernehmen

Uebernehmen: normale Go-Funktionen zur Komposition, kleine Value-Views,
typisierten State und Widgets als Kombination weniger Primitiven.

Nicht uebernehmen: die Gleichsetzung von Renderknoten und Transportprotokoll,
vollstaendige Root-Rebuilds fuer jede visuelle Aenderung, window-weite State-IDs,
Callstack-basierte Identitaet und reflektive Gleichheitspruefung als Standard.

Nagos Browser erledigt Layout, Text, Scrolling und viele Effekte selbst. Gift
muss diese Aufgaben ausdruecklich modellieren; der Desktop-Renderer ist nicht
bloss ein alternativer Ausgabekanal fuer denselben Protokollbaum.

Es wird kein Nago-Code kopiert. Dessen eigene Lizenz ist nicht mit einer
allgemeinen Erlaubnis zur Uebernahme gleichzusetzen.

## 3. Package-Struktur

```text
gift/                       Modulwurzel, Package gift
  geom/                     Geometrie und numerische Einheiten
  ui/                       Views, Modifier und zusammengesetzte Controls
  asset/                    Bildquellen, Collections, Lade- und Cache-Service
  render/                   Backend-Vertrag, Display-Listen, Ressourcen
  backend/
    ebiten/                 Fenster, Eingabe, Text und GPU-Ausgabe
  internal/
    scene/                  Indexierte retained Nodes und Handles
    layout/                 Layoutalgorithmen, Masonry-/Zeilenindex
    text/                   Shaping, Messung, Zeilenumbruch, Glyphenschluessel
  cmd/
    example-counter/
    example-layout/
    example-gallery/
    example-effects/
```

Die Modulwurzel wird als `github.com/torbenschinke/gift` importiert. Kein
weiteres Verzeichnis gift innerhalb des Moduls und kein generischer
pkg/-Sammelordner.

| Package | Verantwortlichkeit | Darf nicht enthalten |
| --- | --- | --- |
| gift | App, View-Vertrag, Build-Context, Scope, State, Binding, Lifecycle, Scheduling | Ebitengine-Typen, Datei-/HTTP-Loader |
| geom | Point, Size, Rect, Insets, Constraints, Transformationen | Views, State, Backend |
| ui | Layout-Views, Text, Image, Button, Gallery, Styles und Material-Deklaration | GPU-Objekte, Netzwerkarbeit |
| asset | Source, Metadata, Collection, asynchrone Pipeline, persistenter Thumbnail-Cache | Views, UI-State, Ebitengine |
| render | Backend-Vertrag, Zeichenoperationen, Textmessung, Ressourcen-Handles, Faehigkeiten | Widgets, Transportprotokoll, App-State |
| backend/ebiten | Ebitengine-Adapter, Glyphenausgabe, Texture-, Target- und Effektverwaltung | Galerie-Layout, fachliche Controls, Shaping-Logik |
| internal/scene | Speicherlayout und Wiederverwendung der Nodes | Abhaengigkeit zur Modulwurzel gift |
| internal/layout | Gemeinsames Layout und raeumliche Indizes | Ebitengine und asset-I/O |
| internal/text | Rendererunabhaengiges Shaping und Zeilenumbruch | GPU-Objekte, Atlasverwaltung |

Vorgesehene Abhaengigkeitsrichtung:

```text
cmd/example-* ---> ui ---> gift ---> render ---> geom
       |           |  \      |
       |           |   \     +----> internal/scene
       |           |    +--------> internal/layout, internal/text
       |           +----> asset
       +----> backend/ebiten ---> gift, render, geom, internal/text
```

`ui` benutzt `internal/layout` und `internal/text` direkt; Layoutalgorithmen
und Zeilenumbruch gehoeren nicht in die Modulwurzel. Die internen Hilfspakete
duerfen render/geom verwenden, aber nicht gift zurueckimportieren. Lifecycle und
Orchestrierung bleiben deshalb zunaechst in gift. Der asset-Service liefert
CPU-Ergebnisse; die UI-Integration reicht diese ueber den rendererneutralen
Ressourcenvertrag weiter.

`internal/text` liefert Shaping-Ergebnisse als Glyph-ID-, Position- und
Font-Referenzlisten. Nur `backend/ebiten` kennt den Glyph-Atlas. Dadurch
benutzen Messung und Ausgabe zwingend dieselben Shaping-Ergebnisse.

Skalierung erfolgt entlang dieser Grenzen, nicht durch ein Package je Widget
oder durch einen presentation/core-Sammelbereich. Loader und Caches koennen
spaeter als Unterpakete von asset ausgegliedert werden, sobald ihre APIs stabil
und eigenstaendig nuetzlich sind. Kein vorsorgliches Aufteilen in mehrere Module.

## 4. Oeffentliche API: Variante A

```go
func Counter(ctx *gift.Context) gift.View {
    count := ctx.State("count", 0)

    return ui.VStack(
        ui.Text("Counter").FontSize(24),
        ui.Text(strconv.Itoa(ctx.Read(count))),
        ui.HStack(
            ui.Button(ui.Text("-"), func() {
                count.Set(count.Get() - 1)
            }),
            ui.Button(ui.Text("+"), func() {
                count.Set(count.Get() + 1)
            }),
        ).Gap(8),
    ).Gap(16).Padding(24)
}
```

- Views sind kurzlebige Beschreibungen. Dauerhafte Identitaet liegt im Scope
  und im retained Baum, nicht in einer einzelnen View-Struct-Kopie.
- Heterogene Kinderlisten verwenden View-Interfaces nur an der Build-Grenze.
  Im Frame-Hotpath wird kein rekursiver Interface-View-Baum ausgewertet.
- Modifier geben konkrete Typen zurueck. Keine gemeinsamen Modifier-Interfaces,
  deren Methoden manche Widgets stillschweigend ignorieren.
- Generische Methoden stehen auf konkreten Typen wie Context. Der View- und
  Backend-Vertrag hat gewoehnliche, nicht generische Interface-Methoden.
- Go-Generics garantieren weder vollstaendige Monomorphisierung noch Inlining
  oder Allokationsfreiheit. Escape-Analyse und Benchmarks entscheiden.

### Styling endet an der View-Grenze

Nachtrag aus WU-C. Variante A hat eine Konsequenz, die der urspruengliche Plan
nicht ausgesprochen hat: **Modifier ueberleben die `gift.View`-Grenze nicht.**
Sie sind auf konkreten Typen deklariert; sobald ein Wert in `gift.View` geboxt
ist, sind sie weg.

Daraus folgt konkret:

- `gift.Component("row", Row).Padding(8)` kompiliert nicht und kann es nicht.
  Der sanktionierte Weg ist `ui.VStack(gift.Component(...)).Padding(8)`, also
  genau der zusaetzliche Knoten, den das Feld-Design bei Blaettern vermeidet.
- Eine Hilfsfunktion `func Card(title string) gift.View` ist vom Aufrufer nicht
  stylebar. Wiederverwendbare Widgets muessen entweder einen konkreten Typ
  zurueckgeben oder Style-Parameter entgegennehmen.
- Die API-Flaeche waechst multiplikativ: jede Modifier-Methode mal jedes Widget.
  Bei Text, Image, Button und Gallery kommen dutzende Einzeiler-Forwarder dazu,
  und der Compiler meldet nicht, wenn einer vergessen wurde.

  **Entschieden nach WU-G: nicht generieren, sondern pruefen.** Mit `Box`,
  `Stack`, `Overlay`, `Spacer` und `Text` liegen rund 70 solcher Einzeiler vor,
  die in §4 genannte Schwelle ist also erreicht. Ein Generator loest aber das
  falsche Problem: der Fehlerfall "Methode vergessen" ist ein Compilerfehler an
  der Aufrufstelle, kein stilles Fehlverhalten. Ein Generator kostet dafuer
  einen Buildschritt, einen `go:generate`-Vertrag und eine zweite Stelle, an
  der man `func (t TextView) Padding` suchen muss.

  Stattdessen: ein Test, der ueber alle exportierten View-Typen reflektiert und
  den gemeinsamen Modifier-Satz mit korrekter Signatur einfordert. Er faengt
  genau den realen Fehler, kostet rund dreissig Zeilen und haelt den Code
  greppbar. Faellig mit WU-H.

Das ist kein Argument fuer gemeinsame Modifier-Interfaces; die bleiben
abgelehnt. Es ist die ehrliche Formulierung des Preises: **Styling ist eine
Eigenschaft der konkreten Konstruktionsstelle, nicht etwas, das man auf eine
beliebige `View` anwenden kann.**

### Ownership der Kinderslices

`ui.VStack(a, b, c)` erzeugt ein frisches, vom Compiler alloziertes Slice; Gift
darf es behalten. `ui.VStack(items...)` uebergibt dagegen das Slice des Aufrufers
ohne Kopie.

Vertrag: **ab Uebergabe gehoert das Slice Gift.** Der Aufrufer darf es weder
veraendern noch weiterverwenden. Wer ein wiederverwendetes Puffer-Slice besitzt,
muss selbst kopieren oder den Builder mit expliziter Kopiersemantik nutzen.
Value-Semantik ist keine Deep-Copy-Zusage. Im Debug-Build wird die Slice-Identitaet
zwischen zwei Builds geprueft und eine Verletzung gemeldet.

### Komponenten

Eine normale Hilfsfunktion erzeugt keinen eigenen State-Scope. Wiederverwendbare
zustandsbehaftete Komponenten werden explizit als Komponenten gemountet:

```go
ui.VStack(
    gift.Component("left-counter", Counter),
    gift.Component("right-counter", Counter),
)
```

## 5. State, Binding und Lifecycle

- `ctx.State("name", initial)` liefert einmalig initialisierten, typisierten
  State innerhalb der aktuellen Komponenteninstanz. Gleiche lokale Keys in
  unterschiedlichen Komponenten sind erlaubt.
- Der Standardweg verwendet vergleichbare Werte und `==`. Fuer andere Werte
  gibt es einen ausdruecklichen Comparator-/Versionsweg, kein DeepEqual-Default.
- `ctx.Read(state)` liest und registriert die Abhaengigkeit zum aktuellen Scope.
  `state.Get()` liest ohne Abhaengigkeit, etwa in einem Eventhandler.
- `state.Set(value)` ist der eindeutige Aenderungsweg. Aenderungen werden bis zum
  naechsten Build zusammengefasst. Kein separates Notify zur normalen Verwendung.
- `state.Binding()` verbindet Lesen und Schreiben fuer Controls, ohne Ownership
  zu uebertragen. Binding-Konsumenten registrieren ihre Abhaengigkeit ebenfalls.
- State lebt bis zum Unmount seines Scopes, nicht bis zum Auslassen eines Lookups.
  Ein gecachter, nicht neu gebauter Teilbaum bleibt gemountet.
- Dynamische Kinder brauchen stabile Modell-Keys. Typwechsel unter gleichem
  State-Key wird mit einer klaren Diagnose abgewiesen.
- Abhaengigkeiten werden nach einem erfolgreichen Build aktualisiert; nicht
  mehr gelesene States invalidieren den Scope danach nicht weiter.
- UI-State gehoert einem UI-Executor. Worker-Ergebnisse werden gepostet und vor
  Annahme auf Scope-/Request-Generation geprueft. Keine Callbacks unter Locks.
- Scrolloffset, Hover, Pressed und Animation sind lokaler Praesentationszustand.
  Sie sollen keinen fachlichen Root-Rebuild erzwingen.
- Dauerhafte Galerieauswahl lebt beim Collection-Owner, nicht im recycelten Tile.

Noch kein allgemeiner Signal-/Effect-Graph, keine implizite Goroutine-Sicherheit
fuer State und keine magische Erkennung von In-place-Mutationen an Maps/Slices.

## 6. Frame-Modell

Dieser Abschnitt haelt fest, was Ebitengine tatsaechlich anbietet, weil das die
Invalidierungsarchitektur bestimmt.

### Ebitengine-Realitaet

- `Update` und `Draw` sind entkoppelt. Vor einem `Draw` koennen mehrere `Update`
  laufen, bei hoher Last auch keiner.
- `Draw` erhaelt den Screen. Standardmaessig wird er jeden Frame geloescht.
  Es gibt **kein** partielles Neuzeichnen des Bildschirms durch Dirty Rects.
- Ebitengine exponiert keine GL-Version, keine Extensions und keine
  GPU-Speicherabfrage. Eine capability-basierte Qualitaetswahl ist nicht moeglich.

### Konsequenz: was Invalidierung spart und was nicht

Gift zeichnet **jeden Frame die vollstaendige sichtbare Display-Liste neu.**
Es gibt keine Composite-Invalidierung im Sinne von "nur der geaenderte
Bildschirmausschnitt wird angefasst". Frueher formulierte Erwartungen dazu
werden hiermit zurueckgenommen.

Invalidierung spart deshalb gezielt CPU-Arbeit, nicht Fuellrate:

| Stufe | Was entfaellt bei Nichtaenderung |
| --- | --- |
| Build | Kein Aufruf der View-Funktionen, keine Interface-Allokationen |
| Layout | Kein Measure/Arrange, keine Neuberechnung des Galerieindex |
| Text | Kein Shaping, kein Zeilenumbruch, keine Glyph-Atlas-Aenderung |
| Paint | Keine Neuerzeugung der Display-Liste, nur Transform-Patch |
| Effekt-Cache | Kein Blur-Rerender fuer Shadow und Glass |

Reines Scrollen darf deshalb weder Text neu formen noch den View-Baum neu bauen.
Es fuehrt aber weiterhin zu einem vollen Zeichendurchlauf der sichtbaren Kacheln.
Das ist die Basislast, gegen die alle Budgets in Abschnitt 13 gerechnet werden.

### Ablauf pro Frame

```text
Update:
  Eingabe einlesen, Gesten aufloesen, Animationen taktieren
  gepostete Worker-Ergebnisse annehmen (Generation pruefen)
  falls Build-Invalidierung: betroffene Scopes neu bauen, reconcilen
  falls Layout-Invalidierung: Measure und Arrange
  Upload-Budget planen

Draw:
  Display-Liste der sichtbaren Nodes erzeugen bzw. wiederverwenden
  Material-Passes (siehe Abschnitt 8)
  Zeichenbefehle absetzen
```

Build und Layout laufen ausschliesslich in `Update`, nie in `Draw`. Uploads
werden pro tatsaechlich gezeichnetem Frame budgetiert, nicht pro `Update`.

### Idle-Verhalten

Ohne Aenderung und ohne laufende Animation zeichnet Gift eine statische Szene
weiterhin 60-mal pro Sekunde, weil Ebitengine das so vorgibt. Fuer Pi-Betrieb
ist das relevant: Dauerlast erzeugt Waerme und damit Throttling. Gegenmassnahme
im ersten Aufschlag ist `ebiten.SetTPS` samt reduzierter Zielrate im Idle als
optionale App-Politik, nicht ein eigener Praesentationspfad. Echtes
On-Demand-Rendering ist **kein** Bestandteil dieses Plans.

Praezisierung aus WU-D, in laufendem Code geprueft: `SetTPS` regelt
ausschliesslich die `Update`-Rate. Die `Draw`-Rate gibt das Display vor. Ein Pi
an 60 Hz bekommt mit `IdleTPS=10` weiterhin 60 `Draw`-Aufrufe je Sekunde. Die
Idle-Politik spart also Build, Layout und Eingabeverarbeitung, **nicht** die
Fuellrate. Das ist dieselbe Trennung wie bei der Invalidierung oben.

Ebitengine hat neben `Update` und `Draw` einen dritten Callback `Layout`. Er
ist die einzige Stelle, an der die Viewportgroesse vor dem ersten `Update`
bekannt ist. Das Backend bezieht sie von dort.

## 7. Layout, Text und Eingabe

```text
Deklaration bei relevanter Aenderung
  -> keyed Reconciliation / retained Nodes
  -> Measure und Arrange bei Layout-Invalidierung
  -> Display-Liste, Clip- und Transformdaten
  -> Backend-Ressourcen und Ausgabe
```

### Overflow-Modell

Nachtrag aus Review-Gate 2. Der urspruengliche Plan hat die Frage "der Inhalt
passt nicht" nicht beantwortet. WU-C hat sie stillschweigend beantwortet, und
zwar falsch: ein Stack hat spaeteren Kindern den verbleibenden Platz als Maximum
gegeben, diese sind auf Hoehe 0 kollabiert, ihre eigenen Kinder wurden aber
weiterhin an ihren echten Positionen gezeichnet und lagen uebereinander. Bei
40 Zeilen in einem begrenzten Viewport kollabierten 30 und stapelten sich in
einem 120-Pixel-Band. Das ist die eine Kombination, die weder ehrlich noch
sicher ist.

Verbindlich gilt ab sofort:

1. **Ein Stack misst unflexible Kinder mit unbegrenzter Hauptachse.** Er
   verteilt keinen Restplatz an sie und verknappt sie nicht der Reihe nach. Die
   Querachse bleibt begrenzt. Damit haengt die Groesse eines Kindes nicht davon
   ab, wie viele Geschwister vor ihm stehen.
2. **Flexible Kinder bekommen den Rest**, tight auf der Hauptachse, anteilig
   nach `Flex`. Ist kein Rest da, bekommen sie 0.
3. **Overflow ist erlaubt und sichtbar.** Ueberschreitet die Summe aus Kindern
   und Gaps den verfuegbaren Platz, behalten die Kinder ihre ehrlichen Groessen
   und Positionen. Der Stack meldet nach oben die von den Constraints erlaubte
   Groesse, aber der Ueberstand wird als Zahl gefuehrt und ist in `Diagnostics`
   sichtbar. Unter `giftdebug` gibt es zusaetzlich eine Diagnose mit Knoten und
   Ueberstand.
4. **Es wird nicht automatisch geclippt.** Wer Overflow abschneiden will, setzt
   `Clip(true)`. Stillschweigendes Clippen wuerde denselben Fehler verstecken,
   den Punkt 3 sichtbar machen soll.
5. Das deckt sich mit dem bereits dokumentierten Vertrag von `gift.Layouter`,
   dass eine constraint-verletzende Groesse unveraendert durchgereicht und nicht
   stillschweigend geklemmt wird. Die bisherige Implementierung hat genau dagegen
   verstossen.

Folgeregel fuer `ui.Box`: ein `Box` ist auf jeder **begrenzten** Achse gierig.
Weil ein Stack die Hauptachse nach Regel 1 unbegrenzt misst, kollabiert ein
`Box` ohne `Frame` oder `Flex` in einem Stack auf der Hauptachse und fuellt die
Querachse. In einem `ZStack` sind beide Achsen begrenzt, also fuellt er beide.
Das ist genau das Verhalten, das die Godocs beider Stellen ohnehin behaupten.

Erste Layoutregeln: numerische logische Pixel, Min-/Ideal-/Max-Groessen,
Padding, Gap, Alignment und Spacer. Text wird mit begrenzter Breite gemessen;
Zeilenumbruch und Baselines sind Bestandteil des gemeinsamen Vertrags.
Keine CSS-Strings und keine unterschiedlichen Stack-Algorithmen je Backend.

Nodes und Zeichenoperationen liegen in wiederverwendbaren Slices mit
indexbasierten Handles und Generationen. Die Lebensdauer geliehener Display-
Listen ist definiert: der Backend-Consumer darf sie nicht nach Wiederverwendung
durch den Producer lesen. Backend-eigene Kopien und GPU-Kommandos sind gesondert
zu budgetieren.

Koordinatenraum von Clip und Transform, festgelegt in WU-D: **Clips sind
Device-Space, Transformationen bilden Bounds nach Device-Space ab.**
`List.PushClip` schneidet entlang eines einzigen Stapels, und das ergibt nur in
einem gemeinsamen Raum Sinn. Sollte die Galerie spaeter in gescrolltem Raum
clippen muessen, ist das eine Verhaltensaenderung und vor Schritt 3 zu
entscheiden, kein Implementierungsdetail.

### Textstack

Shaping erfolgt mit `github.com/go-text/typesetting`, derselben Basis, die auch
Ebitengines `text/v2` verwendet. `internal/text` kapselt sie vollstaendig.

Im ersten Aufschlag enthalten:

- Horizontales LTR-Shaping, Kerning, Ligaturen nach Font-Vorgabe.
- Zeilenumbruch an Wortgrenzen nach UAX 14 in der von typesetting gelieferten Form.
- Shaping-Cache mit Schluessel aus Text, Font, Groesse und Breitenbeschraenkung.
- Glyph-Atlas im Backend mit Eviction nach Bytes und Verwendungsalter.
- Ganzzahlige Baseline-Positionierung; kein Subpixel-Positioning.

Bewusst nicht enthalten: Bidi und RTL, vertikale Schrift, automatische
Fallback-Fontketten ueber mehrere Schriften, Hinting-Varianten, Texteditor, IME.
Die Fallback-Kette ist die wahrscheinlichste erste Erweiterung nach dem MVP;
die API von `internal/text` wird dafuer offen gehalten, aber nicht vorgebaut.

Shaping ist erfahrungsgemaess der groesste Einzelposten in Schritt 2. Er wird
deshalb dort getrennt ausgewiesen.

### Eingabe

Eingabe, Clip und Hit-Testing verwenden dieselben Koordinatentransformationen.

- Maus: Hover, Pressed, Pointer-Capture, Wheel-Scrolling.
- Tastatur: Fokusreihenfolge, Disabled, Aktivierung per Space/Enter,
  Pfeilnavigation in der Galerie.
- Touch: Tap, Long-Press, Drag-Scroll und kinetisches Scrollen mit Reibung.
  Touch nachtraeglich einzubauen waere teuer, weil es die Pointer-Capture- und
  Scroll-Semantik beruehrt.

  **Korrigiert nach WU-H.** Die urspruengliche Begruendung lautete zusaetzlich,
  die Zielhardware sei typischerweise ein Pi-Display. Das ist fuer den
  Desktop-Backend falsch und in der Quelle geprueft: Ebitengine dokumentiert an
  `AppendTouchIDs` selbst "AppendTouchIDs always does nothing on desktops".
  Unter Raspberry Pi OS mit X11/XWayland - der in Abschnitt 1 festgelegten
  Plattform - kommt ein Touchscreen als Maus an. Der Touch-Pfad ist real und
  getestet, wird aber von Android- und iOS-Builds ausgeuebt, nicht vom Pi. Das
  Architekturargument traegt, das Hardwareargument nicht.

  Ebitengine hat ausserdem **keine Tastenwiederholung**. Die Suche im
  gepinnten Modul findet `Repeat` nur im Gamepad-Code. Das Naechstliegende ist
  `inpututil.KeyPressDuration`, das Ticks zaehlt; Anfangsverzoegerung und Rate
  muessten darauf aufgebaut werden. Fuer den ersten Aufschlag ist das ohne
  Belang, weil Space/Enter und Tab auf der Flanke ausloesen. Ein Texteditor
  braeuchte es, und der ist in Abschnitt 14 ausgeschlossen.

Ein gemeinsames Pointer-Modell abstrahiert Maus und Touch; Touch-Ereignisse
erzeugen keine synthetischen Hover-Zustaende. Multitouch beschraenkt sich auf
die Erkennung und Verwerfung zusaetzlicher Finger. Pinch-Zoom, Rotation und
vollstaendige native Accessibility-Bridges sind nicht Teil des MVP.

## 8. Border, Shadow und Glass

Effekte sind rendererneutrale Deklarationen und keine Ebitengine-Shader im
Anwendungscode. Die Schreibweise soll beispielsweise so aussehen:

```go
ui.VStack(
    ui.Text("Library").FontSize(24),
    ui.Button(ui.Text("Import"), importPhotos),
).
    Gap(12).
    Padding(20).
    Background(ui.Glass().Quality(ui.Adaptive)).
    Border(ui.Border{Width: 1, Color: ui.RGBA(255, 255, 255, 90)}).
    CornerRadius(18).
    Shadow(ui.Shadow{Blur: 16, OffsetY: 4, Color: ui.RGBA(0, 0, 0, 70)})
```

### Semantik

- Zunaechst benannte Style-Felder mit definierter Zeichenreihenfolge:
  Shadow, Hintergrund/Material, Inhalt, Border. Wiederholtes Setzen ersetzt den
  jeweiligen Style-Wert. Keine Behauptung identischer SwiftUI-Modifier-Semantik.
- Border liegt innerhalb der Bounds und aendert das Layout nicht.
- CornerRadius bestimmt die Hintergrund-/Borderform; Content-Clipping wird
  explizit eingeschaltet. Ein Glass-Backdrop wird an seiner Materialform geclippt.
- Shadow erweitert die Paint-Bounds, aber nicht Layout oder Hit-Area.
  Eltern-Clips gelten auch fuer den Schatten.
- Farben und Alpha-Konventionen werden an der Backend-Grenze klar festgelegt;
  Ebitengine erwartet premultipliziertes RGBA fuer Pixel-Uploads.

### Warum Glass mehrere Passes braucht

Die naheliegende Frage lautet, warum ein einziger Shader nicht genuegt. Zwei
unabhaengige Gruende:

1. **Kein Framebuffer-Read.** Ein Fragment-Shader kann das Renderziel, in das er
   schreibt, nicht gleichzeitig lesen. GL ES und Kage bieten weder programmierbares
   Blending noch `framebuffer_fetch`. Der Hintergrund muss als samplebare Textur
   vorliegen. Das erzwingt mindestens ein separates Render-Target und ist der
   eigentliche Grund fuer den Mehraufwand, unabhaengig vom Blur.
2. **Blur ist eine weite Faltung.** Ein Gauss mit Radius 16 braucht 33 Taps,
   separiert 2x33. Bei 1080p ist das auf VideoCore VI nicht bezahlbar.

Grund 2 laesst sich weitgehend aufloesen: **Downsampling ist der Blur.** Bei
1/8 Aufloesung wirkt ein 4-Tap-Kernel wie Radius 24 im Vollbild. Mit Dual-Kawase
(4 Taps je Pass, Down- und Up-Kette) kostet die gesamte Kette unter 1,5x der
Flaeche der **Materialregion**, nicht des Bildschirms. Fuer ein 1920x200-Panel
sind das rund 0,6 MPixel statt 66 Taps auf 0,38 MPixel.

Grund 1 laesst sich nicht aufloesen, aber auf genau ein Extra-Target begrenzen:
kopiert wird nur die Materialregion.

### Die zwei Qualitaetsstufen

**Reduced, Standard:** ein einziger Shader-Pass, kein Extra-Target, kein Blur.
Tint, Fresnel-artige Kantenaufhellung, Specular-Highlight und eine leichte,
normalenbasierte Brechungsverschiebung auf dem ungeblurrten Hintergrund. Optisch
ueberraschend nah an Glas und praktisch gratis. Das ist der Default auf Pi 4.

**Full, experimentell:** Regionskopie, Dual-Kawase-Kette auf der Region,
Composite-Pass mit Tint, Brechung, Highlight und Grain. Drei Stufen, alle auf die
Materialregion begrenzt.

| Effekt | Erster Ansatz | Begrenzung |
| --- | --- | --- |
| Border / Radius | Analytische Geometrie im gemeinsamen Shape-Shader | Kein Offscreen-Bild pro Widget |
| Shadow | Wiederverwendbare, gecachte Formmaske; Blur nur bei Form-/Parameterwechsel | Cache nach Bytes, Schattenumfang und Blur-Radius begrenzen |
| Glass Reduced | Ein Shader-Pass, kein Target | Kein echter Hintergrund-Blur |
| Glass Full | Regionskopie + Dual-Kawase + Composite | Experimentell, Flaechen- und Speicherbudget je Frame |

Die Display-Liste muss Materialregionen, Z-Reihenfolge und
Hintergrundabhaengigkeiten ausdruecken. Das Backend erzeugt die Zwischenziele;
kein `ReadPixels` und keine zyklischen Texturabhaengigkeiten.

Der Backdrop enthaelt nur vorher gezeichnete Inhalte, nicht das Material selbst
oder seine Kinder. Beim Scrollen darunter wird er dirty. Ein statischer
Glasrahmen bedeutet deshalb nicht, dass sein Blur wiederverwendbar ist.

### Adaptive ist messungsbasiert

Da Ebitengine keine Capabilities exponiert, kann `Adaptive` nicht
geraetebasiert entscheiden. Es ist eine **messungsbasierte Politik**: ein
gleitendes Fenster der Frametimes und des Materialflaechenbudgets entscheidet
zwischen Full und Reduced, mit Hysterese und einer Mindesthaltedauer je Stufe,
damit kein Flackern entsteht. Die effektive Stufe ist in der Diagnostik sichtbar.
Anwendungen koennen eine Stufe fixieren; fuer vergleichbare Messungen ist das
verpflichtend.

Im ersten Beispiel: ein Glass-Panel ueber scrollender Galerie. Kein Blur fuer
jede einzelne Kachel. Kein Anspruch auf eine pixelgenaue Nachbildung von Apples
Liquid Glass; das eigene Material bleibt experimentell.

## 9. Assets und automatische Bildoptimierung

```go
type Source interface {
    Metadata() Metadata
    Open(context.Context) (io.ReadCloser, error)
}

type Metadata struct {
    ID       ID
    Revision string
    Width    uint32
    Height   uint32
    MIMEType string
}
```

Metadata ist nicht blockierend; unbekannte Felder bleiben leer. Bekannte
Abmessungen beziehen sich auf die orientierte Darstellung. Open liefert einen
frischen Reader; Gift schliesst ihn. Der Reader muss nicht seekbar sein.

`asset.File(path)` und `asset.HTTP(url)` konstruieren Quellen ohne blockierende
I/O. Vor dem Probing gilt: die `ID` wird aus dem bereinigten absoluten Pfad
beziehungsweise der normalisierten URL gebildet, die `Revision` bleibt leer und
wird nach dem Probing aus mtime und Groesse beziehungsweise aus ETag oder
Last-Modified gesetzt. Eine leere Revision bedeutet "noch nicht validiert", nicht
"unveraenderlich". Die Anwendung kann eigene Asset-Objekte liefern. Vorhandene
Provider-Thumbnails koennen spaeter ueber eine optionale Erweiterung genutzt werden.

Eine app-weite Pipeline dedupliziert Requests und verwaltet:

1. Metadaten-Probing und Validierung der Quelle.
2. Priorisierte, bytebegrenzte Fetch-/Decode-Auftraege.
3. Orientierung und Thumbnail-Erzeugung in wenigen Pixelgroessen mit Hysterese.
4. Persistenten Thumbnail-Cache mit konfigurierbarem Verzeichnis und Budget.
5. Begrenzten CPU-Pixelcache und eine begrenzte Ready-Queue.
6. Upload-Admission und explizit freigegebene Backend-Ressourcen.

Sichtbare Bilder haben Vorrang vor richtungsabhaengigem Prefetch. Abgebrochene
Arbeit darf die aktuelle Ansicht nicht ueberschreiben. Content-Version,
Request-Generation und Tile-/Texture-Slot-Generation sind getrennte Konzepte.

Cache-Keys enthalten Revision, Groesse, Orientierung und Verarbeitungsversion.
Dateiquellen werden neu validiert, HTTP nutzt verfuegbare Validatoren und eine
definierte Freshness-Policy. Ohne Validator ist eine URL keine ewige Revision.
Authentifizierte Quellen brauchen getrennte Cache-Namespaces; Zugangsdaten
gehoeren nicht in Dateinamen oder Diagnoseausgaben. Disk-Caching ist abschaltbar.

Jeder Pipelineabschnitt hat ein Bytebudget. Ein Channel-Limit allein genuegt
nicht. Decode-Arbeit reserviert Speicher nach Bildabmessungen und Codec-Risiko,
nicht nach der spaeteren Thumbnailgroesse. Begrenzte komprimierte Eingabedaten,
Pixelgrenzen, Timeouts und Fehler-/Retry-Zustaende gehoeren zum ersten Prototyp.

Die Standarddecoder sind nicht beliebig abbrechbar und allokieren. CPU-Decoding
wird daher nicht als GC-frei bezeichnet. Ein Kontextabbruch verhindert weitere
Arbeit und die Veroeffentlichung veralteter Ergebnisse, kann aber bereits laufende
Codec-Berechnungen nicht generell sofort stoppen.

## 10. Galerie und grosse Collections

```go
func Gallery(ctx *gift.Context, photos *asset.Collection) gift.View {
    return ui.ImageGallery(photos).
        Layout(ui.Masonry().MinColumnWidth(240).Gap(8)).
        OnSelect(func(id asset.ID) {
            // Detailansicht oeffnen.
        })
}
```

ImageGallery besitzt seinen Scrollbereich und verwendet denselben Bildservice
wie `ui.Image(source)`. Sie ist eine Komposition ueber einem internen lazy
Grid-/Viewport-Kern, kein eigener Backend-Zeichenbefehl.

Die Collection liefert geordnete, stabile IDs, Revisionen und Metadatenzugriff
ohne I/O im Frame. Strukturupdates werden versioniert publiziert. Es braucht
keinen State und keinen View pro Katalogeintrag. Die konkrete Collection-API
wird mit den ersten File-/HTTP-Beispielen festgelegt, nicht mit einer Datenbank
oder einem Dateisystem-Scanner gleichgesetzt.

- Masonry: feste Spaltenbreite, variable Hoehe, Einfuegen in die kuerzeste Spalte.
- Justified/Brick: gemeinsame Zeilenhoehe, variable Breiten und gefuellte Zeilen.
  Beide Layouts sind Teil des Galerie-Aufschlags; Masonry wird zuerst umgesetzt.
- Globale Layoutmetadaten duerfen O(N) Speicher brauchen. Sichtbare Nodes,
  Requests und Texturen bleiben durch Viewport und Budgets begrenzt.
- Pro Spalte beziehungsweise Zeile werden sichtbare Intervalle binaer gesucht.
  Der Scrollpfad durchsucht nicht alle 100.000 Eintraege.
- Initiales Layout, Sortierung und Spaltenwechsel duerfen O(N) Arbeit benoetigen;
  sie werden ausserhalb des Frame-Hotpaths oder inkrementell berechnet und
  versioniert uebernommen. Ergebnisse veralteter Layouts werden verworfen.
- Fehlende Abmessungen verwenden vorlaeufige Seitenverhaeltnisse. Korrekturen
  werden gebuendelt; stabile Bild-ID plus lokaler Offset erhalten den Scrollanker.
- Sehr grosse Dokumentpositionen werden erst nach Abzug des Viewport-Ursprungs
  in float32-GPU-Koordinaten konvertiert.
- Schnelle Spruenge zeigen Platzhalter, statt auf Laden oder Decode zu warten.
- Selektion und Tastaturnavigation folgen stabilen IDs und der logischen
  Collection-Reihenfolge, nicht dem zufaelligen Speicherplatz einer Kachel.

100.000 RGBA-Thumbnails mit 256x256 Pixeln benoetigen etwa 24,4 GiB reine
Pixeldaten; 256 solcher Thumbnails etwa 64 MiB. Nur der begrenzte Arbeitsbestand
darf resident sein. Ein Beispielindex mit 32 Byte pro Eintrag benoetigt dagegen
bei 100.000 Eintraegen rund 3,1 MiB, ohne weitere Metadaten und Indizes.

## 11. Performance-Vertrag und bekannte Risiken

Das Ziel lautet: keine laufenden Heap-Allokationen im aufgewaermten Gift-Pfad fuer
unveraenderten Bildbestand und reine Scroll-/Transform-Updates.

Praezisierung, damit der Vertrag pruefbar bleibt. Er gilt fuer den Frame-Pfad
**ohne Build**: Eingabeverarbeitung, Indexsuche, Transform-Patch, Erzeugung der
Display-Liste und Absetzen der Zeichenbefehle. Ausdruecklich **nicht** erfasst,
sondern getrennt gemessen:

- Build. `ui.VStack(a, b, c)` alloziert per Definition das variadische Slice und
  boxt die Kinder in View-Interfaces. Das ist gewollt und deshalb ist Build
  invalidierungsgesteuert, nicht allokationsfrei.
- Neu sichtbare Tiles, Bild-Decoding, Thumbnail-Erzeugung.
- Backend-interne Allokationen von Ebitengine.
- **Shaping-Cache-Misses.** Praezisierung aus WU-F. Textmessung findet im
  Layout statt, und Layout ist Teil des Vertrags. Der Vertrag gilt aber nur
  fuer bereits geshapten Text: ein Cache-Hit ist mit 0 B/op gemessen, ein Miss
  alloziert rund 5 KB in harfbuzz und laesst sich ohne eigenen Shaper nicht
  vermeiden. Der Vertrag lautet also genau: **Layout ist allokationsfrei fuer
  Text, den es schon gesehen hat.**

  Das ist kein Wortspiel, sondern eine Abnahmebedingung. Ein Label, das sich
  aendert, ist ein Build und damit ohnehin ausgenommen. Eine Beschriftung, die
  sich ohne Rebuild aendert, oder eine Galerie, die staendig neue Bildtitel in
  den Viewport scrollt, verfehlt den Vertrag dagegen in jedem Frame. Schritt 3
  bekommt dafuer ein eigenes Messszenario; das darf keine Entdeckung waehrend
  der Galeriearbeit werden.

Kein globales GC-Abschalten, keine unsafe-Arena als Ausgangspunkt.

- Ebitengine 2.10.1 baut auf Desktop ohne CGO; Linux braucht weiterhin native
  Laufzeitbibliotheken, Display-Server und einen funktionierenden Grafiktreiber.
- Der OpenGL-Uploadpfad kann glFinish vor Pixel-Updates ausfuehren. Hintergrund-
  Decoding allein garantiert daher keine ruckelfreien Uploads.
- Uploads erhalten ein Budget pro gezeichnetem Frame, nicht pro Update-Aufruf.
  Ebitengine kann mehrere Updates vor einem Draw ausfuehren.
- CPU-Zeit am Upload-Aufruf ist wegen interner Queues kein GPU-Zeitmass.
- Zunaechst automatischer Ebitengine-Atlas und explizites Deallocate bei Eviction.
  Logische Pixelbytes sind kein exaktes GPU-Budget: Padding, Fragmentierung,
  Atlaswachstum, Zwischenziele und Staging brauchen zusaetzlichen Speicher.
- Falls noetig, wird ein begrenzter Pool eigener Atlas-Seiten verglichen.
  Keine vorsorgliche eigene Atlas-Engine ohne Messung.
- Keine Vollbild-Textur pro Widget, kein GPU-Readback im Framepfad, kein globales
  Umsortieren transparenter Inhalte nur zur Verringerung von Draw Calls.
  Die Glass-Regionskopie aus Abschnitt 8 ist die einzige zugelassene Ausnahme
  und auf die Materialregion begrenzt.
- GOMEMLIMIT begrenzt nicht GPU-/Treiber-/Gesamtprozessspeicher. Gerade beim Pi
  konkurrieren CPU und GPU um gemeinsamen physischen Speicher.
- Vollredraw jedes Frames ist die Basislast. Fuellratenprobleme lassen sich nicht
  durch bessere Invalidierung loesen, sondern nur durch weniger oder kleinere
  ueberlappende Zeichenoperationen.

## 12. Implementierung in pruefbaren Schritten

Die Groessenangaben sind grobe Kalibrierung fuer eine Person, keine Zusagen.
Schritte 1 bis 3 sind zusammen ein vollstaendiges Retained-Mode-Toolkit samt
eigenem Textstack; das ist Personenmonate, keine Sprints.

### Schritt 1: Fundament und Stacks

Ebitengine 2.10.1 pinnen, CGO-freien Desktop-Build und Linux/arm64-Cross-Build
pruefen. View-/Scope-Vertrag, State/Binding, retained Speicher, Frame-Modell aus
Abschnitt 6 und Backend-Vertrag implementieren. VStack, HStack, Spacer, Frame,
Padding und Alignment umsetzen.

Ergebnis: example-layout mit stabiler Identitaet und rendererunabhaengigen
Layout-/State-Tests. Keine leeren Public-Packages nur zur Vorwegnahme des Plans.

**Go/No-Go vor Schritt 2.** Alle vier Kriterien muessen erfuellt sein:

1. `CGO_ENABLED=0` Build fuer host und linux/arm64 erfolgreich.
2. Fenster laeuft auf Pi 4 mit einer nichttrivialen statischen Stack-Szene
   stabil bei 60 fps.
3. Allokationsbenchmark des Frame-Pfads nach Warmup bei 0 B/op.
4. Layout- und State-Tests laufen ohne Fenster und ohne GPU.

Wird eines verfehlt, wird der Plan revidiert statt fortgesetzt.

### Schritt 2: Text, Button und einfache Effekte

**Zurueckgenommen: das `fwidth`-Risiko.** Die vorherige Revision hat
Screen-Space-Ableitungen im Shape-Shader als "Unbekannten mit der groessten
Varianz" gefuehrt, mit der Begruendung, Ebitengine koenne auf dem Pi auf
GLSL ES 1.00 zurueckfallen, wo `dfdx`/`dfdy` die Erweiterung
`OES_standard_derivatives` verlangen. **Das war falsch und ist in der Quelle
geprueft.** Ebitengine 2.10.1 hat gar keinen ES-1.00-Pfad:
`internal/shaderir/glsl` kennt genau zwei Versionen und emittiert `#version
150` oder `#version 300 es`. In GLSL ES 3.00 sind Ableitungen Kernsprache. Die
Suche nach `ES100`, `#version 100` und `OES_standard_derivatives` im ganzen
Modul liefert nichts.

Der Shader kommt in WU-E trotzdem ohne Ableitungen aus, aber aus anderen
Gruenden, und der damals befuerchtete Ausweichweg war ebenfalls ein Irrtum: es
braucht keinen zusaetzlichen Vertex-Slot und keinen zweiten Shader. Die
Lokal-nach-Device-Skalierung ist auf der CPU bekannt und wird in die fuenf
ohnehin uebertragenen Werte eingerechnet. Danach ist das Distanzfeld in
Device-Pixeln gemessen, die AA-Breite ist konstant 1, und `fwidth` entfaellt.
Das hat nebenbei einen echten Fehler behoben - die Vertex-Polsterung war in
lokalen Einheiten und schnitt unter Verkleinerung die aeussere Haelfte jeder
geglaetteten Kante ab - und die Artefakte an den Knicken des Distanzfeldes
beseitigt.

**Das tatsaechliche Pi-Risiko** ist, ueberhaupt keinen ES-3.0-Kontext zu
bekommen. Das betraefe den gesamten Renderer und nicht nur abgerundete Formen,
und kein Shader-Umbau hilft dagegen. Pi 4 und Pi 5 koennen es mit Mesa v3d;
zu pruefen bleibt es trotzdem, aber als gewoehnlicher Plattformtest und nicht
als vorgezogenes Designrisiko.

Lehre fuers Vorgehen: diese Passage stand drei Revisionen lang im Plan, weil
eine plausible Behauptung eines Agenten ungeprueft uebernommen wurde. Fuer
Aussagen ueber Fremdcode gilt ab sofort dasselbe wie fuer Messwerte - Beleg
aus der Quelle oder sie stehen nicht im Plan.

Groesster Einzelposten des Projekts. Shaping-Anbindung an go-text/typesetting,
Messung, Shaping-Cache, Glyph-Atlas und Eviction. Danach Button-Interaktion,
Fokus und das gemeinsame Pointer-Modell inklusive Touch. Border, Radius und
gecachte Shadows integrieren; Paint-Bounds und Clip-Semantik testen.

Ergebnis: example-counter und erweiterte Layout-Demo, einschliesslich
Tastatur- und Touch-Bedienung. Noch kein Texteditor, kein Bidi, kein
Font-Fallback.

### Schritt 3: Galerie ohne I/O

100.000 synthetische Metadatensaetze, virtuelle Masonry- und anschliessend
Justified-Layouts, Platzhalter, Scrollanker, Selektion, kinetisches Scrollen und
Recycling umsetzen.

Ergebnis: example-gallery kann deterministisch scrollen, springen, umkehren,
resizen und Layouts wechseln. Der reine Scrollpfad ist unabhaengig von N
abgesehen von der Indexsuche.

### Schritt 4: Vollstaendige Image-Pipeline

File-/HTTP-Source, Metadaten, Orientierung, Thumbnail-Erzeugung, Disk-/CPU-/GPU-
Caches, Abbruch und Upload-Budgets integrieren. ui.Image und Galerie teilen
Ressourcen. Zunaechst JPEG und PNG mit dokumentierter Orientierungsbehandlung;
weitere Formate nur ueber klar registrierte Decoder. Kein RAW-/Video-Support.

Ergebnis: example-gallery zeigt reale lokale und HTTP-Bilder, inklusive
Cold-Loading, Fehlern und dauerhaftem Thumbnail-Cache.

### Schritt 5: Glass und integrierter erster Aufschlag

Materialregionen in der Display-Liste, Reduced-Material als
Einzelpass-Shader, danach Full-Material mit Regionskopie und
Dual-Kawase-Kette. Messungsbasierte Adaptive-Politik mit Hysterese.
example-effects zeigt Border, Shadow und ein Glass-Panel ueber der Galerie.

Erst nach diesem Schritt ist der hier vereinbarte erste Aufschlag vollstaendig.
Eine isolierte Counter-Demo gilt nicht als Erfuellung dieses Plans.

## 13. Tests, Budgets und Review-Kriterien

Automatisierte Korrektheitstests:

- State-Key-Isolation, Unmount, geaenderte Abhaengigkeiten und stale Async-Resultate.
- Slice-Ownership-Verletzung wird im Debug-Build erkannt.
- Stack-Constraints, Text-Baselines, Zeilenumbruch, Modifier-Zeichenreihenfolge
  und Paint-Bounds.
- Pointer-Capture, Fokus, Disabled, Scroll-/Clip-transformiertes Hit-Testing,
  Touch-Drag gegen Tap-Abgrenzung.
- Galerieindex gegen eine einfache Vollsuche, inklusive Random-Jumps und Resize.
- Keine falschen Bilder nach Tile-/Slot-Recycling und Quellenrevisionen.
- Bytebudgets und Freigabe bei Fehler, Abbruch, Queue-Saettigung und Shutdown.
- HTTP-Validatoren, defekte/zu grosse Bilder, unbekannte Abmessungen und Orientierung.
- Shadow-/Glass-Invalidierung bei bewegtem Hintergrund; Full-/Reduced-Fallback
  inklusive Hysterese ohne Flackern.

Build-/Benchmark-Pruefungen:

- go test und go vet fuer die implementierten Packages; Race-Tests fuer CPU-Pipeline.
- CGO_ENABLED=0 fuer lokalen Desktop und Linux/arm64-Cross-Build.
- Tests des Kerns ohne Fenster oder GPU; Renderer-Tests separat mit Grafik-Kontext.
- Allokationsbenchmarks nach Warmup fuer Scrollen, Indexsuche und Transformationen.
- Allokationsbenchmark des Frame-Pfads mit aktivem Debug-Level-Logger: 0 B/op.

### Abnahmeschwellen

Referenz ist Pi 4, Raspberry Pi OS 64-Bit, 1920x1080 bei 60 Hz, fixierte
Qualitaetsstufe, ohne Throttling. Durchschnitts-FPS ist kein Kriterium.

Definition "verpasstes Intervall", festgelegt in WU-D. Das nominale Intervall
bei 60 Hz ist 16,667 ms. Ein reales Display trifft das nie exakt, und ein
strikter Vergleich gegen 16,67 ms hat in einer sauber getakteten Messung
50 % der Frames als verpasst gemeldet. Verbindlich ist deshalb eine Toleranz
von **0,5 ms**: ein Intervall gilt als verpasst, wenn es **17,17 ms**
ueberschreitet. Nominalwert, Toleranz und daraus folgende Schwelle werden in
jeder Messausgabe mitgefuehrt, damit die Zahl nachvollziehbar bleibt.

Ebenfalls verbindlich, ergaenzt in WU-C2, weil beides `missed_ratio`
materiell verschiebt und eine Messung sonst nicht zwischen Laeufen
vergleichbar ist:

- **Aufwaermen.** Die ersten 60 Intervalle werden verworfen. Das Oeffnen des
  Fensters erzeugt auf der Referenzmaschine ein Intervall von 123 bis 148 ms;
  bei 4096 Ringplaetzen kann ein 60-Sekunden-Szenario diesen Ausreisser nie
  verdraengen. Die Zahl ist auf der Zielhardware einmal nachzumessen, dort ist
  das Aufwaermen vermutlich laenger.
- **Sub-Frame-Intervalle.** Intervalle unter 1 ms sind zwei `Draw`-Callbacks
  direkt hintereinander, keine zwei Praesentationen. Sie werden verworfen und
  **getrennt gezaehlt**. Stilles Verwerfen waere derselbe leise Optimismus wie
  eine falsch gerundete Perzentile.
- Verworfene Aufwaermframes und Sub-Frame-Intervalle stehen als eigene Zahlen
  in jeder Messausgabe.

Perzentile werden nach Nearest Rank gebildet, nicht kaufmaennisch gerundet.
Kaufmaennisches Runden lag in 282 von 900 geprueften Faellen eine Probe zu
niedrig, also systematisch optimistisch genau am Tail, ueber den das Kriterium
entscheidet. `p99,9` ist bei 60 Hz erst ab etwa 1000 Proben aussagekraeftig und
entartet darunter zum Maximum; das ist in der Ausgabe kenntlich zu machen.

| Szenario | Kriterium |
| --- | --- |
| Scroll 60 s, 100k Platzhalter | p99-Frametime < 16,67 ms; < 1 % verpasste Intervalle |
| Scroll 60 s, warme reale Bilder | p99 < 16,67 ms; < 2 % verpasste Intervalle |
| Kalter Cache, Scroll 60 s | p99,9 < 33 ms; keine Stalls > 100 ms |
| Frame-Pfad ohne Build, aufgewaermt | 0 B/op im Allokationsbenchmark |
| Build eines Counter-Scopes | 0 Allokationen im Anteil von gift; siehe unten |
| Glass Reduced ueber Galerie | Zusatzkosten < 1,0 ms je Frame |
| Glass Full ueber Galerie | Zusatzkosten < 4,0 ms je Frame; Materialflaeche <= 25 % des Screens |
| Shadow, 20 sichtbare Instanzen | Zusatzkosten < 0,5 ms je Frame nach Cache-Aufwaermung |
| Langes Scrollen, 10 min | RSS-Plateau; Wachstum < 5 % ueber die letzten 5 min |
| GPU-Bildspeicher, 100k Eintraege | geschaetzt < 256 MiB resident |

Pi 5 wird getrennt gemessen und darf strengere Werte erreichen; er ersetzt die
Pi-4-Abnahme nicht. Auf Pi 4 ist `Glass Full` ausdruecklich als "darf die
Schwelle verfehlen" gekennzeichnet; dann greift Reduced als Default und der
Fehlschlag wird dokumentiert, nicht versteckt.

Korrektur zur Build-Schwelle, belegt in WU-B2. Die urspruengliche Vorgabe
"< 8 Allokationen pro Build" war nicht erreichbar und ist zurueckgenommen. Der
Counter aus Abschnitt 4 hat einen strukturellen Boden von neun Allokationen,
die der Aufrufer erzeugt, bevor gift beteiligt ist: zwei variadische
Kinderslices, fuenf Boxings von Kindwerten in `View` und zwei Closures fuer die
Buttons. Das ist Variante A und deckt sich mit Abschnitt 11, der Build
ausdruecklich vom 0-B/op-Vertrag ausnimmt. Gemessen wird deshalb der Anteil von
gift selbst; er muss 0 sein. Die Gesamtzahl inklusive View-Konstruktion wird
danebengestellt und beobachtet, aber nicht als Schwelle gefuehrt.

example-gallery und example-effects erhalten reproduzierbare Szenarien und
maschinell lesbare Messausgaben. Erfasst werden Build-/Layout-Aufrufe, sichtbare
Nodes, Uploadbytes, Queuealter, Cache-Hits, Platzhalterdauer, Allokationen, Heap,
RSS soweit verfuegbar und geschaetzter GPU-Bildspeicher. CPU-Zeiten,
Draw-/Swap-Intervalle und echte GPU-/Praesentationszeiten werden nicht vermischt.

Auf der Zielhardware getrennt pruefen:

| Szenario | Aussage |
| --- | --- |
| 10k / 100k Platzhalter | Kosten des Index und der Virtualisierung |
| Warme reale Bilder | Rendering, Batching und Cache-Stabilitaet |
| Kalter Thumbnail-/Datei-/HTTP-Cache | Decode, I/O, Upload-Stalls, GC und Nachladezeit |
| Schnelle Spruenge und Richtungswechsel | Priorisierung, Abbruch und Recycling |
| Resize und Layoutwechsel | Reflow, Scrollanker und alte Layoutresultate |
| Border / Shadow / Reduced Glass / Full Glass | Isolierte Effektkosten |
| Langes Scrollen, Minimieren, Wiederherstellen | Speicherplateau und Lifecycle |

Mesa, Display-Stack, RAM, Datentraeger, Aufloesung, Skalierung und
Temperatur/Throttling protokollieren. Eine Budgetverletzung muss sichtbar sein;
keine stillschweigende Verringerung der Bildanzahl oder Effektqualitaet im
Benchmark.

## 14. Bewusst nicht im MVP

Damit der Umfang nicht unbemerkt waechst, ausdruecklich ausgeschlossen:

Bidi/RTL, vertikale Schrift, Font-Fallback-Ketten, Texteditor, IME, Subpixel-
Positioning, Multiwindow, native Accessibility-Bridges, Pinch-Zoom und Rotation,
RAW- und Videoformate, Theming/Dark-Mode-System, Fraktionale DPI-Skalierung,
Internationalisierung der Beispiele, On-Demand-Rendering, eigene Atlas-Engine,
Signal-/Effect-Graph, Animationskurven jenseits einfacher Interpolation.

Fehler-/Logging-Strategie und API-Stabilitaet sind in Abschnitt 15 geregelt.

## 15. API-Stabilitaet, Fehler und Logging

### API-Stabilitaet

Die API ist **vorerst nicht stabil**. Es gibt keine Deprecation-Frist, keine
Migrationshilfen und keine Zusage zu Signatur-, Namens- oder
Verhaltenskonstanz. Breaking Changes duerfen jederzeit und ohne Ankuendigung
erfolgen, auch innerhalb eines Schrittes.

Das Modul bleibt auf v0. Es wird kein `/v2`-Pfad und kein Stabilitaetsversprechen
in der Dokumentation behauptet.

Der Uebergang zu stabiler API erfolgt **ausschliesslich auf ausdrueckliche
Ansage des Projekteigners**, nicht automatisch nach Abschluss von Schritt 5 und
nicht abgeleitet aus dem Reifegrad. Bis dahin ist "das hat sich geaendert" kein
Fehlerbericht. Ab dieser Ansage wird dieser Abschnitt durch eine konkrete
Kompatibilitaetspolitik ersetzt.

### Fehlerbehandlung

- Programmierfehler im Vertrag von Gift werden mit Panic und klarer Diagnose
  abgewiesen, nicht stillschweigend toleriert: Typwechsel unter gleichem
  State-Key, Verletzung des Slice-Ownership, State-Zugriff ausserhalb des
  UI-Executors, Build waehrend `Draw`.
- Nicht jede dieser Pruefungen ist im Release-Build aktiv. Typwechsel, Build
  waehrend `Draw` und Re-Entranz panicken immer. Slice-Ownership,
  Duplikat-Keys und die UI-Executor-Pruefung sind nur unter dem Build-Tag
  `giftdebug` aktiv, weil ihre Erkennung sonst den 0-B/op-Vertrag aus
  Abschnitt 11 im Eventhandler-Pfad verletzen wuerde. Fuer nebenlaeufige
  Fehler ist ohnehin `-race` das verbindliche Werkzeug, nicht diese
  Laufzeitpruefung.
- Laufzeitfehler aus der Aussenwelt sind normale Werte: I/O, HTTP, Decode,
  Cache. Sie werden ueber den asset-Vertrag zurueckgegeben und fuehren zu einem
  sichtbaren Fehlerzustand der betroffenen Kachel, nie zum Abbruch des Frames.
- Ein Fehler erzeugt keinen Retry-Sturm. Fehlgeschlagene Quellen bekommen einen
  Backoff und werden bis zur Revisionsaenderung nicht erneut geladen.
- Gift ruft in keinem Pfad `os.Exit` oder `log.Fatal`.

### Logging

Regel: **Logging darf die Performance nicht beeinflussen.** Daraus folgt eine
harte Trennung.

**Frame-Hotpath: kein Logging.** In `Update` und `Draw` wird nicht geloggt, auch
nicht auf Debug-Level und auch nicht hinter einer Levelpruefung. Der Grund ist
nicht nur die Ausgabe, sondern die Konstruktion der Attribute: jedes
`slog.Any`/`slog.String` im Hotpath erzeugt Allokationen und verletzt den
0-B/op-Vertrag aus Abschnitt 11. Eine Verletzung faellt im Allokationsbenchmark
des Go/No-Go auf.

**Stattdessen: Zaehler.** Der Frame-Pfad schreibt ausschliesslich in eine
vorallozierte Diagnostik-Struktur mit einfachen numerischen Feldern, ohne
Formatierung, ohne Interface-Boxing, ohne Locks im Normalfall. Sichtbare Nodes,
Uploadbytes, Cache-Hits, verpasste Intervalle und die effektive Glass-Stufe sind
Zaehler, keine Logzeilen.

Die Zaehler werden ausserhalb des Hotpaths abgegriffen: entweder durch die
Anwendung per Snapshot-Methode oder durch einen optionalen Diagnose-Sink mit
niedriger Rate, hoechstens einmal pro Sekunde. Nur dort entsteht ein
`slog.Record`. Die maschinenlesbaren Messausgaben aus Abschnitt 13 speisen sich
aus diesem Snapshot, nicht aus Logparsing.

**Ausserhalb des Hotpaths: `log/slog`.** Lifecycle, Asset-Pipeline, Cache,
Backend-Initialisierung und Fehlerpfade verwenden slog. Fuer Domain- und
Anwendungscode ist slog verbindlich; Gift gibt keine eigene Logger-Abstraktion
und kein eigenes Logger-Interface vor.

- Gift loggt nie nach `slog.Default()`. Der Logger wird bei der App-Konstruktion
  uebergeben; ohne Angabe ist Logging aus, nicht "Default".
- `context.Context` wird in der asset-Pipeline durchgereicht, damit die
  Anwendung ihren Handler mit Requestbezug anreichern kann.
- Kosten entstehen nur bei aktivem Level: teure Attribute werden hinter
  `Logger.Enabled` oder `slog.LogValuer` gebildet, nie unbedingt.
- Keine Zugangsdaten, Tokens oder vollstaendigen authentifizierten URLs in
  Logs oder Cache-Dateinamen, siehe Abschnitt 9.
- Levelgebrauch: `Error` nur fuer Fehler, die der Anwender sehen muss;
  `Warn` fuer degradierte Qualitaet, etwa Rueckfall auf Glass Reduced;
  `Info` fuer Lifecycle und Konfiguration; `Debug` fuer Pipeline-Details.
  Kein `Info` pro geladenem Bild bei 100.000 Eintraegen.

Getestet wird: ein Allokationsbenchmark des Frame-Pfads mit aktivem
Debug-Level-Logger muss weiterhin 0 B/op liefern.

## 16. Im Review zu bestaetigen

1. Modulpfad `github.com/torbenschinke/gift`, Go 1.27, Ebitengine 2.10.1.
2. Frame-Modell: Vollredraw jedes Frames, Invalidierung spart CPU-Arbeit und
   nicht Fuellrate, kein On-Demand-Rendering.
3. Package-Grenzen: kleine Modulwurzel fuer Runtime-Vertraege, ein ui-Package,
   eigenstaendiges asset und render, genau ein konkretes Backend.
4. Style-Semantik: feste Zeichenreihenfolge statt positionsabhaengiger SwiftUI-
   Modifier-Wrapper im ersten Aufschlag. Slice-Ownership geht an Gift ueber.
5. Effekte: Border/Shadow regulaer, Glass Reduced als Default-Einzelpass,
   Glass Full experimentell mit Regionskopie und Kawase-Kette, Adaptive
   messungsbasiert. Keine Apple-Pixelparitaet, keine Full-Glass-60-fps-Zusage.
6. Text: go-text/typesetting, LTR ohne Bidi und ohne Font-Fallback im MVP.
7. Eingabe: Maus, Tastatur und einfache Touch-Gesten inklusive kinetischem
   Scrollen gehoeren in den ersten Aufschlag.
8. Galerie: Masonry und Justified/Brick, automatische lokale/HTTP-Bildpipeline,
   Platzhalter als bewusstes Verhalten bei fehlenden Bildern.
9. Abnahmeschwellen aus Abschnitt 13 und das Go/No-Go nach Schritt 1.
10. Plattformumfang und Ausschlussliste aus Abschnitt 14.
11. API vorerst instabil, Stabilisierung nur auf ausdrueckliche Ansage.
    Kein Logging im Frame-Hotpath, Zaehler statt Logzeilen, `log/slog`
    ausserhalb des Hotpaths und verbindlich im Domain-Code.

## Quellen der Architekturpruefung

- Nago, untersuchter Stand: https://github.com/worldiety/nago/tree/76fb9e89b1169c9f6bc020a5b31971422898479c
- Nago View-Vertrag: https://github.com/worldiety/nago/blob/76fb9e89b1169c9f6bc020a5b31971422898479c/presentation/core/component.go
- Nago State: https://github.com/worldiety/nago/blob/76fb9e89b1169c9f6bc020a5b31971422898479c/presentation/core/state.go
- Go 1.27, generische Methoden, August 2026: https://go.dev/doc/go1.27
- Ebitengine 2.10/2.10.1: https://ebitengine.org/en/documents/2.10.html
- Ebitengine Desktop-Voraussetzungen: https://ebitengine.org/en/documents/install.html
- Image-Lifecycle und Upload-API: https://github.com/hajimehoshi/ebiten/blob/v2.10.1/image.go
- OpenGL-Upload und glFinish: https://github.com/hajimehoshi/ebiten/blob/v2.10.1/internal/graphicsdriver/opengl/image.go
- Update-/Draw-Vertrag: https://github.com/hajimehoshi/ebiten/blob/v2.10.1/run.go
- Kage-Shadersprache: https://ebitengine.org/en/documents/shader.html
- go-text/typesetting: https://github.com/go-text/typesetting

Die Machbarkeitsbewertung basiert auf Architektur- und Quellpruefung, nicht auf
bereits ausgefuehrten Pi-Benchmarks.
