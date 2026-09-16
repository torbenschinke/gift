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
  metrics/                  Messung und Bericht, hinter Build-Tag giftmetrics
  gifttest/                 Testharness fuer Anwendungen, siehe Abschnitt 13
  font/
    inter/                  Eingebettete Schrift, optional, siehe Abschnitt 17
    ibmplexmono/            Eingebettete Schrift, optional, siehe Abschnitt 17
  icon/
    outline/                Eingebettete Icons, optional, siehe Abschnitt 21
    solid/                  Eingebettete Icons, optional, siehe Abschnitt 21
  clipboard/                Zwischenablage per purego, optional, Abschnitt 19
  internal/
    scene/                  Indexierte retained Nodes und Handles
    layout/                 Layoutalgorithmen, Masonry-/Zeilenindex
    text/                   Shaping, Messung, Zeilenumbruch, Glyphenschluessel
    stress/                 Parametrisierte Lastszene als Fixture
    example/                Gemeinsame Hilfen der Beispiele
    icon/                   Icon-Format, Rasterer und Stroker
    iconsvg/                SVG-Teilmenge, nur fuer den Generator
  cmd/
    example-counter/
    example-gallery/
    example-effects/
    gift-stress/
    gift-icongen/
```

Stand nach Review-Gate 8. `metrics`, `gifttest` und `internal/stress` sind
nach dem urspruenglichen Entwurf dazugekommen und standen bisher nicht im
Baum. `cmd/example-layout` ist entfallen; siehe Abschnitt 12 Schritt 2.

`font/*`, `icon/*` und `clipboard` sind mit den Abschnitten 17, 21 und 19
dazugekommen. Sie enthalten Bytes und eine Registrierung, sonst nichts:
keine Views, kein Layout, keine Ein-/Ausgabe. Sie importieren `ui`, weil der
Typ, den sie liefern oder registrieren, dort liegt; sie stehen damit
**oberhalb** von `ui`, in derselben Stellung wie `cmd/`. Kein Paket innerhalb
von gift nennt sie, sie sind ausschliesslich ueber einen Side-Effect-Import
oder eine ausdrueckliche Nennung durch die Anwendung erreichbar. Die Richtung
der Abhaengigkeiten aus diesem Abschnitt bleibt dadurch unveraendert.

`internal/iconsvg` ist bewusst intern und wird nur vom Generator benutzt: es
ist eine Teilmenge von SVG fuer genau einen Korpus und keine Zusage, SVG zu
koennen.

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

Praesentationszustand, der einen Neuaufbau ueberleben muss, liegt in der
Knoten-Nutzlast und nicht im View, im Layouter oder im Painter: diese drei
werden bei jedem Aufbau neu erzeugt. Fuer das Scrollen ist das
`ScrollIndicatorState`, fuer Steuerelemente `ControlState` — Griffpunkt einer
laufenden Geste, Ausgangswert und Startzeitpunkt einer laufenden Animation.
Geschrieben wird er aus dem Interactor und aus dem Layouter; der Layoutdurchlauf
ist die einzige Stelle, an der ein Knoten bemerken kann, dass ein *Aufbau*
seinen Wert veraendert hat. Der Painter liest ihn und schreibt ihn nie.

Zwei Regeln dazu, beide aus Review-Gate 12 und beide teuer erkauft. Erstens:
ein Nutzlastfeld, das nicht bei jedem Aufbau aus dem Element aufgefrischt wird,
wird beim **Unmount** in `nodeData.release()` geleert, neben `scroll`, `ia` und
`xform` — nicht beim Mount. Der Unterschied ist sichtbar, weil die Nutzlast
recycelt wird: ein Test, der einen Knoten in *einem* Update austauscht, trifft
den recycelten Platz nie, weil der Abgleich erst alle neuen Kinder mountet und
danach die uebrig gebliebenen abraeumt. Ein solcher Test ist gruen, auch wenn
das Leeren ganz fehlt. Zweitens: gift stellt einem deaktivierten Knoten keine
Ereignisse zu. Ein Steuerelement, das waehrend einer Geste deaktiviert wird,
bekommt sein Loslassen also nie und kann sich nicht selbst aufraeumen. Die
Geste wird dort beendet, wo die Deaktivierung angewandt wird. Ein Wachposten
im Widget selbst ist dort unerreichbar und darum kein zusaetzlicher Schutz,
sondern eine irrefuehrende Behauptung.

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
bleibt die Kette auf die **Materialregion** begrenzt statt auf den Bildschirm.
Fuer ein 1920x200-Panel sind das rund 0,6 MPixel statt 66 Taps auf 0,38 MPixel.

Zwei Korrekturen aus WU-S:

- **"Unter 1,5x" ist arithmetisch unerreichbar** und widersprach dem eigenen
  Rechenbeispiel oben. Die Down-Kette schreibt 1/4 + 1/16 + 1/64, die Up-Kette
  dasselbe rueckwaerts **plus die volle Region**. Bei drei Stufen, was dem
  Standardradius entspricht, sind das 0,33 + 1,31 = **1,64x der Region**. Die
  genannten 0,6 MPixel auf 0,384 MPixel Region sind selbst 1,56x. Verbindlich
  ist die Arithmetik, nicht der Satz. Mit Kopie und Composite kostet das ganze
  Material rund 3,6x die Region.
- **Ebitengine filtert fuer Kage-Shader immer nearest.** `DrawTrianglesShaderOptions`
  hat kein Filter-Feld, und `internal/graphicsdriver/opengl/context.go:232`
  setzt `GL_NEAREST` fest; lineare Filterung wird nur in den eingebauten Shader
  hineingeneriert. "Downsampling ist der Blur" stuetzt sich aber auf die
  bilineare Mittelung, die ein eigener Shader damit nicht bekommt. Ein
  lehrbuchmaessiger 5-Tap-Down-Pass verwirft gegen einen Nearest-Sampler die
  Haelfte der Quelle und kriecht, sobald der Inhalt scrollt. Down- und Up-Pass
  muessen die Mittelung selbst bilden und kosten deshalb acht Fetches statt
  fuenf.

Grund 1 laesst sich nicht aufloesen, aber auf genau ein Extra-Target begrenzen:
kopiert wird nur die Materialregion.

**Korrektur aus WU-S, in der Quelle geprueft.** Der letzte Satz stimmt so nicht.
Ebitengine laesst den Screen gar nicht als Shader-Quelle zu:
`internal/atlas/image.go:732` panickt mit "atlas: a screen image cannot be
created as a source". Die Regionskopie braucht also eine Quelle, die es erst
zu erzeugen gilt. Gift rendert einen Frame, der ein Material enthaelt, in ein
bildschirmgrosses Offscreen und blittet es. Das kostet **ein
bildschirmgrosses Target, ein Clear und ein Blit je Frame**, und zwar nur
solange ein Material sichtbar ist. Abschnitt 11s "Keine Vollbild-Textur pro
Widget" gilt weiterhin - es ist eine je Fenster, nicht je Widget -, aber das
Kostenmodell dieses Abschnitts war zu guenstig und ist hiermit korrigiert.

### Die zwei Qualitaetsstufen

**Reduced, Standard:** kein Blur. Tint, Fresnel-artige Kantenaufhellung,
Specular-Highlight und eine leichte, normalenbasierte Brechungsverschiebung auf
dem ungeblurrten Hintergrund. Das ist der Default auf Pi 4.

**Korrektur aus WU-S: "ein einziger Shader-Pass, kein Extra-Target" war
widerspruechlich.** Die Brechung tastet den Hintergrund ab, und das setzt
voraus, dass er als Textur vorliegt - was Grund 1 oben selbst feststellt.
Reduced kostet daher eine Regionskopie und einen Composite-Pass. Ohne Brechung
waere es ein Pass, saehe aber nicht nach Glas aus.

Und zur Wirkung, ehrlicher als die Vorgaenger-Revision: Reduced liest sich als
Glas, **weil sich der Hintergrund am Rand biegt**, nicht wegen des Tints. Ueber
einem flachen Hintergrund ist es ein leicht getoentes Rechteck mit hellen
Kanten, weil nichts da ist, was sich biegen koennte. Das behebt kein Shader.
Die Formulierung "optisch ueberraschend nah an Glas" wird auf diesen Befund
zurueckgenommen.

**Full, experimentell:** Regionskopie, Dual-Kawase-Kette auf der Region,
Composite-Pass mit Tint, Brechung, Highlight und Grain. Drei Stufen, alle auf die
Materialregion begrenzt.

| Effekt | Erster Ansatz | Begrenzung |
| --- | --- | --- |
| Border / Radius | Analytische Geometrie im gemeinsamen Shape-Shader | Kein Offscreen-Bild pro Widget |
| Shadow | Wiederverwendbare, gecachte Formmaske; Blur nur bei Form-/Parameterwechsel | Cache nach Bytes, Schattenumfang und Blur-Radius begrenzen |
| Glass Reduced | Regionskopie + Composite-Pass | Kein echter Hintergrund-Blur |
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

**Korrektur aus WU-N: die obige Skizze ist so nicht baubar.** `ui.ImageGallery(photos)`
unterstellt, dass die Galerie aus einer View-Konstruktion entsteht. Ein
View-Wert wird aber bei jedem Build weggeworfen, und was ueberleben muss, ist
der Index ueber 100.000 Eintraege samt Tile-Bindungen - 2,4 MB, deren
Neuaufbau je Build O(N) waere. Richtig ist
`ui.ImageGallery(ui.NewGallery(photos))`: das Galerieobjekt gehoert der
Anwendung und ist langlebig, die View ist die kurzlebige Deklaration darauf.
Nebeneffekt: die dauerhafte Selektion liegt damit automatisch beim
Collection-Owner, wie Abschnitt 5 es verlangt.

**Tiles werden gepoolt, nicht ge- und entmountet.** Scrollen bindet Slots eines
positionsbasierten Pools im Layout um, statt Knoten zu mounten. Das ist nicht
nur billiger, es macht den in Abschnitt 13 geforderten Test
"keine falschen Bilder nach Tile-Recycling" ueberhaupt erst moeglich: eine
Umsetzung, die mountet und entmountet, recycelt nichts, und der benannte
Fehlerfall kann dort gar nicht auftreten. Preis, ehrlich benannt: Scrollen
loest ein Relayout der Galerie aus, wo ein gewoehnlicher Scroller nur neu
zeichnet. Deshalb ist das Verhalten per `ScrollSpec.Virtual` opt-in und faellt
`ui.ScrollView` nicht zur Last.

**Der Runtime-Vertrag wurde dafuer erweitert.** Virtualisierung war mit der
bestehenden `gift`-API nicht ausdrueckbar. Neu, Stand nach WU-O:
`ScrollSpec.Virtual`, `LayoutContext.RequestLayout`, `RequestBuild`,
`AnchorScroll`, `Invalidator`, `Node`, `Pass` und `gift.ScrollInteractor`.
Das ist die Naht, die jeder virtualisierende Container braucht, und keine
Galerie-Spezialitaet - aber es ist eine echte Verbreiterung des Vertrags und
gehoert deshalb hier vermerkt.

Zwei Einschraenkungen dazu, aus Review-Gate 4 und bewusst offengelassen:

- `AnchorScroll` (frueher `SetScrollOffset`) schreibt waehrend des Layouts ohne
  Invalidierung. Das ist nur solange gefahrlos, wie nichts stromabwaerts den
  Offset bereits gelesen hat. Statt sich auf Disziplin zu verlassen, panickt
  die Methode jetzt, sobald der Layouter schon ein Kind gemessen hat - die
  Bedingung ist damit erzwungen und nicht bloss dokumentiert.
- `ScrollSpec.Virtual` ist als Eigenschaft des Scrollens benannt, ist aber
  eine Eigenschaft des *Layouters*: es bedeutet nur "markiere Layout statt
  Paint". Es gibt genau einen Konsumenten und keinen zweiten plausiblen, der
  nicht ebenfalls eine virtualisierte Sammlung waere. Es bleibt vorerst, gilt
  aber nicht als abgeschlossener Teil der oeffentlichen Flaeche.

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

  Praezisierung aus WU-M: die Abfrage ist **nicht** O(log N + k), und das kann
  sie bei Masonry auch nicht sein. Eine Suche je Spalte ergibt
  O(c · log(N/c) + k), wobei c eine Konstante des Viewports ist und nicht von
  N abhaengt. Gemessen bei hundertfachem N: Bruteforce 96-fach, Masonry
  4,2-fach, Justified 1,7-fach. Der Rest ueber dem reinen log-Term sind
  Cache-Misses auf einem 2,4-MB-Array. In absoluten Zahlen kostet die Abfrage
  bei 100.000 Eintraegen 1,2 µs (Masonry) beziehungsweise 0,2 µs (Justified)
  von 16,67 ms. Sollte das je stoeren, ist der Hebel ein zusammenhaengendes
  `y`-Array je Spalte, nicht ein anderer Algorithmus.
- Die Abfrage ist nur deshalb allokationsfrei, weil sie an ein Slice des
  Aufrufers anhaengt. Das ist ein Vertrag **an den Aufrufer** und muss bis in
  die Galerie durchgehalten werden.
- Initiales Layout, Sortierung und Spaltenwechsel duerfen O(N) Arbeit benoetigen;
  sie werden ausserhalb des Frame-Hotpaths oder inkrementell berechnet und
  versioniert uebernommen. Ergebnisse veralteter Layouts werden verworfen.
- Fehlende Abmessungen verwenden vorlaeufige Seitenverhaeltnisse. Korrekturen
  werden gebuendelt; stabile Bild-ID plus lokaler Offset erhalten den Scrollanker.

  Praezisierung aus WU-M: der Anker ist **geteilt**. Der lokale Offset lebt im
  Layoutindex, die stabile Bild-ID in der Collection. Der Index fuehrt bewusst
  keine IDs mit: das kostete acht Byte je Eintrag plus eine ID-nach-Position-
  Abbildung, und die Collection besitzt beides ohnehin. Beim Resize und bei
  Korrekturbuendeln genuegt der Offset allein; nur eine Umsortierung braucht
  zusaetzlich die ID.

  Ausserdem offen gelassen und jetzt entschieden: ein Korrekturbuendel
  verschiebt das bereits uebernommene Layout **nicht**. Wann der Inhalt
  nachrueckt, ist eine Ankerentscheidung und gehoert der Galerie. Sonst
  ruckelte der Viewport mitten im Frame, ohne dass vorher ein Anker genommen
  wurde.
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
  den Viewport scrollt, verfehlt den Vertrag dagegen in jedem Frame.

  Stand nach WU-N: **dieses Messszenario hat noch keinen Gegenstand.** Die
  Galerie zeichnet bewusst keine Kacheltitel, gerade weil ein Titel je Kachel
  den Vertrag in jedem Frame brechen wuerde. Die Identitaet einer Kachel ist
  stattdessen ueber `Gallery.Bindings` abfragbar. Sobald Kacheltitel dazukommen
  - sei es in Schritt 4 oder spaeter - muss die Messung zusammen mit ihnen
  kommen und nicht danach.

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
  **Korrigiert nach WU-S.** Es gibt zwei zugelassene Ausnahmen, nicht eine.
  Die Regionskopie aus Abschnitt 8 ist auf die Materialregion begrenzt. Dazu
  kommt ein **bildschirmgrosses Szenen-Target**, weil Ebitengine den Screen
  nicht als Shader-Quelle zulaesst und die Backdrop-Quelle deshalb erst
  erzeugt werden muss. Es ist eine Textur **je Fenster**, nicht je Widget, und
  wird nur bezahlt, solange ein Material sichtbar ist - nicht bloss, solange
  eines in der Display-Liste steht. Wer diesen Unterschied nicht erzwingt,
  zahlt bei 1080p acht Megabyte, ein Clear und einen Vollbild-Blit je Frame
  fuer ein Panel, das niemand sieht.
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

Ergebnis: ein Layout-Beispiel mit stabiler Identitaet und rendererunabhaengigen
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

Praezisierung nach Review-Gate 6: die beiden sind **nicht** zwei Beispiele.
Der Counter ist das Beispiel und heisst auch so; die belastbare Layout-Demo
ist `internal/stress` mit `cmd/gift-stress`, wo Parametrisierung der Zweck ist
und headless-Assertions moeglich sind. Ein zweites Schaubeispiel, das nur
Rechtecke zeigt, waere fuer einen Leser wertlos gewesen - das war der
urspruengliche Zustand von `example-layout` und der Grund, es zu ersetzen.

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

### Paket `gifttest`: Testwerkzeug fuer Anwender

Erweiterung ueber den urspruenglichen Plan hinaus, beauftragt nach Schritt 2.
Dieser Abschnitt regelte bisher nur, wie Gift **sich selbst** testet. `gifttest`
ist das Gegenstueck fuer Anwendungen, die auf Gift aufbauen, im Geist von
Playwright.

- **Headless ist der Normalfall.** Layout, Paint und Assertions laufen auf der
  Display-Liste, ohne GPU und ohne Fenster, also in gewoehnlicher CI mit
  `go test ./...`. Pixelvergleiche sind optional hinter `giftgpu`.
- **Selektoren statt Koordinaten.** `ByText`, `ByKey`, `ByType`, kombinierbar.
  Koordinatengebundene Tests sind genau das, was vermieden werden soll. Dafuer
  traegt `gift.Element` ein `Label`-Feld; das ist bewusst im Runtime-Vertrag
  und nicht in `ui`, weil ein Selektor ueber den ganzen Baum laufen muss und
  eine spaetere Accessibility-Bruecke dieselbe Information braucht. Es kostet
  im Frame-Pfad nichts und ist so gemessen.
- **Deterministische Zeit.** `App.BeginInput(now)` nimmt die Zeit als Parameter,
  also sind Long-Press und spaeter Animationen ohne `time.Sleep` testbar.
- **Fehlermeldungen sind das eigentliche Produkt.** Ein mehrdeutiger Selektor
  nennt alle Treffer mit Bounds, markiert sie im Baumauszug und nennt den
  unterscheidenden Vorfahren - der zugleich die Loesung ist. Ein Null-Treffer
  nennt die tatsaechlich vorhandenen Labels, Keys oder Typen. `Settle` bricht
  eine Endlos-Rebuild-Schleife mit Diagnose ab, statt zu haengen.
- **Golden-Bilder** mit Toleranz 4 je Kanal und **ohne Ausreisser-Budget.** Ein
  Prozentsatz erlaubter Abweichung ist genau der Mechanismus, der eine
  verschobene Form durchwinkt: eine versetzte Kante trifft nur die Pixel entlang
  einer Linie. Geprueft: zwei Pixel Versatz erzeugen 4,1 % abweichende Pixel bei
  maximaler Differenz 255. Bei Fehlschlag werden Ist-Bild und Diff geschrieben
  und benannt, bei Erfolg wieder entfernt.
- **Strukturelle Assertions haben Vorrang vor Bildern.** Ein Golden sagt "das
  hat sich geaendert", nie "das ist falsch", und laeuft im Normalbuild gar
  nicht. Assertions auf der Display-Liste sagen warum.
- **Aktionen pruefen ihr Ziel.** Eine Aktion rechnet sich den Punkt aus den
  Bounds des Knotens aus und steigt zum naechsten interaktiven Vorfahren auf,
  damit ein Test nicht die Interna eines Widgets festschreibt. Danach wird per
  Hit-Test geprueft, **ob der Klick auch dort ankommt**. Review-Gate 3 hat
  belegt, dass ohne diese Pruefung ein Test einen verdeckten Button anklicken
  und trotzdem gruen sein kann. Der bewusste Klick durch etwas hindurch ist
  `ClickAt` mit expliziter Koordinate.

### Bekannte Falle: `t.Parallel` und prozessweiter Zustand

Benannt, nicht behoben, mit Absicht. `internal/text` haelt einen prozessweiten
Shaper samt Shaping-Cache, und `ui.SetDefaultFont` ist prozessweit
veraenderlich. Solange kein Test `t.Parallel` aufruft, ist nichts kaputt; das
Repository enthaelt heute keinen einzigen.

Warum nicht jetzt beheben: ein Mutex saesse auf dem Messpfad, der im Layout
laeuft und damit im 0-B/op-Vertrag aus Abschnitt 11 - und braechte einer
Einfenster-Anwendung nach Abschnitt 1 nichts. Die saubere Loesung ist ein
Shaper je `App`, und die scheitert daran, dass der Layout-Vertrag einem
Layouter ein `*LayoutContext` und keinen Anwendungs-Handle gibt. Das ist eine
Aenderung an `gift.Layouter`, keine Nachbesserung.

Ausloeser zum Beheben: sobald eine zweite `App` im selben Prozess gebraucht
wird, etwa ein Vorschaufenster in Schritt 3. Dann ist es ein
Korrektheitsproblem und kein Testkomfortproblem, und der Shaper je `App` wird
richtig gebaut statt mit einem Lock geflickt. Bis dahin gilt Abschnitt 15:
fuer nebenlaeufige Fehler ist `-race` das verbindliche Werkzeug.

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

Bidi/RTL, vertikale Schrift, Font-Fallback-Ketten, IME im Sinne der
CJK-Komposition, Subpixel-Positioning, Multiwindow, native
Accessibility-Bridges, Pinch-Zoom und Rotation, RAW- und Videoformate,
Fraktionale DPI-Skalierung, Internationalisierung der Beispiele,
On-Demand-Rendering, eigene Atlas-Engine, Signal-/Effect-Graph.

Fehler-/Logging-Strategie und API-Stabilitaet sind in Abschnitt 15 geregelt.

### Aufgehobene Ausschluesse

Der erste Aufschlag ist abgeschlossen. Der Auftraggeber hat danach fuenf
Punkte angefordert, die hier ausgeschlossen waren. Diese Liste beginnt mit
dem Satz, dass der Umfang nicht unbemerkt wachsen soll; also wird das
Wachstum hier bemerkt und begruendet, statt im Code zu passieren.

- `Texteditor` faellt. Begruendung: Abschnitt 19. Was bleibt, ist `IME` im
  engeren Sinn, also Kandidatenfenster und Komposition fuer CJK. Umlaute,
  AltGr und Dead Keys sind kein IME, sondern die normale Uebersetzung von
  Tastendruecken in Zeichen, die jede Tastatur ausserhalb der USA braucht.
  Der bisherige Text hat beides in einen Topf geworfen; das war falsch.
- `Theming/Dark-Mode-System` faellt, aber nur zur Haelfte: Abschnitt 20
  fuehrt semantische Farben und einen Hell-/Dunkel-Umschalter ein. Ein
  `System` im Sinne vererbter Umgebungswerte entsteht ausdruecklich nicht,
  weil Abschnitt 4 ab Zeile 189 dagegen argumentiert und dieses Argument
  weiter gilt.
- `Fraktionale DPI-Skalierung` bleibt ausgeschlossen und wird praezisiert.
  Ausgeschlossen war nie die Geraetedichte als solche, sondern der
  gebrochene Faktor. Ganzzahlige Dichte war nirgends verboten und nirgends
  vorgesehen; sie war eine Luecke, die dieser Ausschluss zugedeckt hat.
  Abschnitt 18 schliesst sie.
- `Animationskurven jenseits einfacher Interpolation` faellt. Das
  Liquid-Glass-Beispiel braucht sie, und Abschnitt 8 hat die Kostenseite von
  Glass bereits vermessen.
- `Font-Fallback-Ketten` bleibt ausgeschlossen. Abschnitt 17 bringt ein
  Font-Register, das ist nicht dasselbe: das Register waehlt eine Schrift
  anhand von Familie, Gewicht und Stil, es setzt keine zweite Schrift fuer
  fehlende Glyphen ein.

Nicht aufgehoben, sondern nie enthalten: Zwischenablage und
Bildschirmtastatur kommen in diesem Dokument bis hier nicht vor. Sie sind
in Abschnitt 19 geregelt.

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
- **Vorlaeufig gegenueber dauerhaft ist ein Typ, kein Kommentar.** Nachtrag aus
  Review-Gate 5. Die Pipeline unterscheidet sorgfaeltig zwischen Fehlern, die
  ein erneuter Versuch behebt (Saettigung, Backoff), und solchen, die er nicht
  behebt (kein Bild, zu gross, kaputte URL) - und reichte dem Konsumenten dann
  ein blankes `error` in einem Feld. **Beide** Konsumenten haben daraufhin eine
  voruebergehende Ablehnung dauerhaft gemerkt: ein schneller Scroll vergiftete
  die Queue, die Queue lehnte ab, und die Kachel blieb fuer immer leer.

  Konsequenz: `Result` hat kein Feld mehr, das "ein Fehler" bedeutet. Es hat
  `Retry` und `Failure`, und beide Namen sagen, was zu tun ist. `Err` ist eine
  Methode, sodass `if res.Err != nil` nicht mehr durch `go vet` kommt. Die
  Regel dahinter gilt ueber diesen Fall hinaus: **wo zwei Pakete sich ueber die
  Bedeutung eines Fehlers einig sein muessen, gehoert die Unterscheidung in den
  Typ und nicht in die Dokumentation.** Ein dritter Konsument soll den Fehler
  nicht wiederholen koennen, nicht bloss davor gewarnt werden.

  Nachtrag dazu: nicht jeder Backoff ist voruebergehend. Eine unter Quarantaene
  stehende Quelle liefert Backoff und kommt ohne Revisionsaenderung nie wieder
  frei. Waere die als wiederholbar eingestuft worden, haette ein 404 in einem
  stehenden Fenster eine abgelehnte Anfrage und ein Relayout **je Frame,
  dauerhaft** erzeugt - eine leere Kachel gegen eine Endlosschleife getauscht.
  Deshalb ist Quarantaene ein eigener, endgueltiger Fehler.
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

## 17. Fonts und Font-Register

Die Beispiele suchen heute jeweils mit einer eigenen, dreimal kopierten
`loadFont` vier Systempfade ab und fallen auf einen CWD-relativen Pfad
zurueck. Das ist aus drei Gruenden zu ersetzen, und nur der erste ist
offensichtlich:

1. Auf einem minimalen Pi-OS-Image greift kein einziger Pfad.
2. Die *gewaehlte* Schrift unterscheidet sich je Plattform, also ist jedes
   Pixelergebnis der Beispiele konstruktionsbedingt maschinenabhaengig.
3. `gifttest` kann Anwendern keine stabilen Goldens anbieten. Die Testschrift
   liegt unter `internal/` und wird ueber einen relativen Pfad geladen; beides
   ist ausserhalb dieses Moduls nicht erreichbar.

### Register statt Einzelslot

`ui.SetDefaultFont` setzt eine einzige globale Variable. Zwei
Side-Effect-Importe wuerden einander in nicht festgelegter
`init`-Reihenfolge lautlos ueberschreiben. Deshalb ein Register, das eine
Schrift unter Familie, Gewicht und Stil aufnimmt, mit `gift.RegisterType` als
Vorbild fuer die Registrierung zur Paketinitialisierung.

Das Register loest *aus den registrierten Schriften* auf. Es ist keine
Fallback-Kette: fehlt ein Glyph in der gewaehlten Schrift, wird keine zweite
Schrift befragt. Abschnitt 14 haelt diesen Ausschluss aufrecht.

### Mitgelieferte Schriften

Zwei optionale Pakete, die nichts kosten, solange sie nicht importiert
werden. Das ist dasselbe Argument, das `internal/stress` fuer seine
eingebettete Schrift bereits fuehrt.

- Inter, statische Instanzen aus dem Upstream-Release v4.1.
- IBM Plex Mono.

Belegte Einschraenkung, die die Quelle der Schriften bestimmt:
`go-text/typesetting` v0.3.5 erkennt in `font/opentype/reader.go` die
Signatur `wOFF`, aber nicht `wOF2`. Die Schriften im untersuchten
Nago-Stand liegen fuer Inter ausschliesslich als WOFF2 vor (Magic
`774f4632`); IBM Plex Mono liegt dort zusaetzlich als WOFF1 vor. Inter wird
deshalb nicht aus dieser Quelle uebernommen, sondern als TTF vom Upstream
bezogen. Ein WOFF2-Decoder in Go waere Brotli plus die
glyf-Ruecktransformation und steht in keinem Verhaeltnis zum Nutzen.

`gifttest.Options` erhaelt ein Font-Feld, damit ein Golden nicht mehr davon
abhaengt, was irgendein anderer Test zuletzt global gesetzt hat.

## 18. Geraetedichte

Die Zielplattform ist ein Pi bei 1920x1080, also Dichte 1. Auf einer
Entwicklungsmaschine mit Retina-Display ist die Ausgabe sichtbar unscharf.
Die Ursache ist nicht kosmetisch:

`backend/ebiten/run.go` implementiert `Layout(int, int) (int, int)` und gibt
die Groesse unveraendert zurueck. Ebitengine rendert damit in ein Bild in
logischer Groesse und skaliert es gefiltert auf den physischen Framebuffer.
`DeviceScaleFactor` wird nirgends aufgerufen. Glyphen rastern bei
Punktgroesse, und der Renderer dokumentiert die Annahme, der Massstab sei
eins, mit `FilterNearest` fuer Glyphen.

Verbindlich:

- `LayoutF` statt `Layout`, Dichte aus `ebiten.Monitor().DeviceScaleFactor()`
  beim Start und bei Monitorwechsel.
- Ein Dichte-Transform an der Wurzel der Display-Liste. Die Shaderseite ist
  darauf vorbereitet: `deviceScale` backt Radius, Strichbreite, AA-Rand,
  Schattensigma und Refraktion bereits aus dem Transform.
- Glyphen rastern in Geraetepixeln. Der Atlas-Key traegt bereits eine
  Groesse; er traegt sie dann in Geraetepixeln.
- Die Bildleiter waehlt ihre Sprosse in Geraetepixeln. Heute waehlt sie nach
  der Layoutgroesse in Punkten, ein Foto ist also auf einem 2x-Display
  unabhaengig vom Framebuffer-Problem um den Faktor zwei unterversorgt.

Ganzzahlige Faktoren sind zugesagt. Gebrochene Faktoren bleiben nach
Abschnitt 14 ausgeschlossen; trifft gift einen solchen, rundet es und
dokumentiert das Ergebnis, statt Genauigkeit zu behaupten, die es nicht hat.

Ein Steuerelement, das nur aus Rechtecken mit Radius besteht, rechnet selbst
**nicht** mit der Dichte. Die Skalierung steht in genau einem Transform an der
Wurzel der Display-Liste, und der Shader backt Radius, Strichbreite und
AA-Rand daraus. Ein Widget, das zusaetzlich runden wuerde, rundet zweimal.
Die Dichte betrifft nur, was gerastert wird: Glyphen, Icons und die
Bildleiter. Auch Layout und Zeigereingabe bleiben durchgehend logisch — eine
Trefferflaeche von 44 Pixeln ist bei jeder Dichte dieselbe Flaeche.

## 19. Texteingabe, Zwischenablage und Bildschirmtastatur

### Der Rune-Kanal ist die Voraussetzung

`backend/ebiten/input.go` fragt dreizehn fest verdrahtete physische
Tastencodes mit `IsKeyPressed` ab. Ebitengines eigene Dokumentation haelt zu
dieser Funktion fest, dass ein `Key` eine physische Taste des US-Layouts
bezeichnet, und stellt `AppendInputChars` als die
lokalisierungsabhaengige Uebersetzung nach Unicode daneben. gift ruft
`AppendInputChars` nirgends auf.

Die Folge ist nicht, dass Umlaute kaputt sind. Die Folge ist, dass **kein
druckbares Zeichen das Framework erreicht**. Ein perfektes `ui.TextField`
bekaeme null Zeichen. Der Rune-Kanal ist daher der erste Schritt und die
Voraussetzung fuer alles Weitere in diesem Abschnitt.

Dazu: `gift.Key` wird um Backspace, Delete und die fuer Kurzbefehle noetigen
Buchstaben erweitert. Tastenwiederholung baut gift selbst, weil Ebitengine
keine hat; das steht seit dem ersten Aufschlag in diesem Dokument, bisher
mit dem Zusatz, ein Texteditor braeuchte sie und sei ausgeschlossen. Der
Zusatz entfaellt, die Aussage bleibt.

### Zwischenablage ohne cgo

Ebitengine 2.10.1 hat keine oeffentliche Zwischenablage-API. Die
GLFW-Implementierung liegt unter `internal/` und ist nach den Importregeln
von Go dauerhaft unerreichbar. Eine externe Abhaengigkeit ist also
unvermeidlich; sie darf aber den cgo-freien Kern nicht brechen, weil
`CGO_ENABLED=0 GOOS=linux GOARCH=arm64` zur Abnahmematrix gehoert.

Deshalb purego, als optionales Paket per Side-Effect-Import. `purego` ist
bereits indirekte Abhaengigkeit ueber Ebitengine. purego fuehrt Linux auf
amd64 und arm64 als Tier 1 und bringt fuer `!cgo` einen eigenen
dlopen-Pfad mit; auf Darwin liegt ein vollstaendiger ObjC-Runtime bei.

- macOS: `NSPasteboard` ueber den ObjC-Runtime. Kein Eventloop.
- Linux/X11: `libX11.so.6` per dlopen, mit einer **eigenen**
  Display-Verbindung und einem unsichtbaren Fenster auf einem eigenen
  OS-Thread. Das ist keine Umstaendlichkeit, sondern X11: wer kopiert, wird
  Owner der `CLIPBOARD`-Selection und muss `SelectionRequest` bedienen,
  solange der Inhalt gelten soll. Ebitengines Verbindung ist dafuer nicht
  erreichbar.
- Windows: Best-Effort. Der Plattformumfang nach Abschnitt 1 kennt Windows
  nicht als Ziel.

Zwei Dinge werden nicht zugesagt: das INCR-Protokoll fuer grosse Transfers
entfaellt zunaechst, die uebertragbare Groesse wird gedeckelt und
dokumentiert. Und der Inhalt verschwindet unter X11 mit dem Prozessende;
das ist Standardverhalten und kein Fehler.

### Bildschirmtastatur

Ebitengine bietet weder IME noch eine Moeglichkeit, eine native
Bildschirmtastatur zu oeffnen. Auf dem Kioskziel existiert ohnehin keine.
gift zeichnet sie also selbst, als Overlay im vorhandenen `ZStack`,
eingeblendet auf `EventFocusGained`. Fuer das Freihalten des Eingabefelds
dient das bereits vorhandene und getestete `ScrollIntoView`.

Zwei Praezisierungen, nach der Umsetzung nachgetragen. Beide standen hier zu
knapp, und beide wurden beim Bauen gefunden, nicht beim Planen:

`ScrollIntoView` allein genuegt nicht, und zwar aus einem Grund der
Reihenfolge, nicht des Koennens. Wenn das Feld um Freistellung bittet, ist es
in der Eingabephase; die Tastatur wird erst danach im selben Frame gebaut und
vermessen. Die erste Freistellung hat also nichts, wovor sie ausweichen
koennte. Es braucht daher eine Kennzeichnung des verdeckenden Knotens und
eine nachgezogene zweite Freistellung. Das ist Verkabelung, damit der hier
genannte Mechanismus zum richtigen Zeitpunkt laeuft, und kein zweiter
Mechanismus.

Das Overlay kann ein Feld nicht freistellen, das die **letzte** Zeile eines
Formulars ist. Ein Container kann nur so weit heben, wie er noch scrollen
kann, und unterhalb der letzten Zeile ist kein Inhalt mehr. Ein
Content-Inset, der den Scrollbereich waehrend der Einblendung verlaengert,
waere die Loesung von iOS und Android; er greift aber in `ScrollInfo`, die
Proportionen des Scrollbalkens, die Overscroll-Verkettung und die
Fling-Begrenzung ein und ist damit nicht klein. Er bleibt vorerst
ausgeschlossen. Stattdessen gilt: das Overlay ist die richtige Anordnung,
wenn hinter der Tastatur kein Scrollcontainer liegt; liegt dort einer, ist
die Spaltenanordnung exakt, weil der Viewport dann wirklich schrumpft. Beide
Wege sind dokumentiert und getestet, und die Hoehe der Tastatur ist abfragbar,
damit eine Anwendung sich stattdessen Platz lassen kann.

**Die Einblendung haengt an einem ausdruecklichen Kioskschalter, nicht an
`PointerKind`.** Begruendung, und sie ist belegt: Ebitengines Dokumentation
zu `AppendTouchIDs` haelt fest, dass die Funktion auf Desktops nichts tut;
dieser Umstand steht bereits in `backend/ebiten/input.go` und in
Abschnitt 7. Auf Raspberry Pi OS mit X11 meldet sich ein Touchscreen als
Maus. Eine an `PointerTouch` gebundene Einblendung wuerde auf genau der
Hardware nie ausloesen, fuer die sie gebaut wird.

## 20. Semantische Farben

Es gibt heute keine benannte Farbe. Jede Farbe ist ein Literal an der
Aufrufstelle, und jede Anwendung erklaert ihre Palette als Paketvariablen.

Eingefuehrt werden benannte Farben, die aus einem prozessweiten Thema
aufloesen, und ein Umschalter zwischen hell und dunkel zur Laufzeit.

Ausdruecklich **nicht** eingefuehrt wird ein Umgebungsmechanismus, der Werte
den Baum hinunterreicht. Abschnitt 4 ab Zeile 189 haelt fest, dass Styling
eine Eigenschaft der konkreten Konstruktionsstelle ist und nicht etwas, das
man auf eine beliebige View anwenden kann. Dieses Argument gilt
unveraendert. Ein Themenwechsel loest einen Neuaufbau ueber `App.Invalidate`
aus.

Eine Nebenwirkung, die mitgebaut werden muss: `ButtonStyle` kennt kein
"dieses Feld wurde nicht gesetzt", und Abschnitt 8 nennt genau das als Grund,
warum Zustandsstile ganze Boxstile ersetzen statt feldweise zu mischen. Ein
Thema laesst sich unter einen teilweise gesetzten Stil nur legen, wenn es
diese Unterscheidung gibt. Das Muster dafuer existiert bereits als
`hasFG` beim Textvordergrund.

## 21. Icons

Icons werden auf der CPU gerastert und als Bild gezeichnet. Es entsteht
**kein** Pfad-Primitiv in der Display-Liste. Begruendung, belegt:

- Der eine Shader ist ein Rounded-Box-SDF, und alle vier
  Vertex-Custom-Attribute sind belegt. Die Datei haelt selbst fest, dass ein
  fuenfter Wert bereits im Shader neu berechnet werden musste, weil kein
  Slot mehr frei war. Uniforms scheiden aus, weil sie in Ebitengine eine
  `map[string]any` sind und pro Operation allozieren wuerden.
- Ein zweiter Shader ist ein zweites Material und damit ein zusaetzlicher
  Draw-Call, also genau das, was der Ein-Shader-Entwurf vermeidet.
- Ein `OpPath` braeuchte eine Seitentabelle nach dem Vorbild der Glyphen und
  acht weitere Bytes im `Op`. Das waere die dritte Vergroesserung.

Der CPU-Weg kostet dagegen nichts Neues: `golang.org/x/image/vector` ist
bereits direkte Abhaengigkeit und wird in `internal/text` genau dafuer
verwendet. Eine Deckungsmaske mit Farbe erst zur Zeichenzeit ist dort
bereits das tragende Prinzip, und `OpImage.Color` ist ein Multiply-Tint,
also faerbt eine weisse Maske mal Vordergrundfarbe korrekt und
kantengeglaettet.

Die SVGs werden zur Bauzeit per `go:generate` zu Pfadsegmenten vorgeparst
und eingebettet. Damit entsteht kein SVG-Parser im Frame-Pfad und keine
neue Abhaengigkeit. Gerastert wird pro Groesse und zwischengespeichert; der
Frame-Pfad bleibt bei 0 B/op nach Abschnitt 11.

Der vorhandene Glyph-Atlas wird **nicht** wiederverwendet. Dieser Absatz hat
frueher das Gegenteil verlangt; die Umsetzung hat gezeigt, dass die Forderung
nicht erfuellbar ist, ohne dem heissesten Cache des Renderers einen Zweig
hinzuzufuegen. Der Atlas ist kein Deckungscache mit glyphfoermigem
Schluessel, sondern ein Glyphcache: sein Schluessel ist
`{FontID, Groesse, GlyphID}`, und sein Miss-Pfad schlaegt eine `*text.Font`
nach und rastert einen Umriss aus deren Tabellen. Ein Icon hat weder Font
noch Glyph-ID noch Umriss; es koennte den Atlas nur als Glyph einer Schrift
passieren, die es nicht gibt. Auch die Regalpackung ruht auf einer Annahme,
die Text erfuellt und Icons nicht: dass alle Glyphen einer Schrift in einer
Groesse aehnlich hoch sind.

Icons gehen deshalb durch `render.Images`, das genau diese Form von Problem
schon bedient: Residenz, ein Uploadbudget pro gezeichnetem Frame, Verdraengung
nach Alter und Bytes, und eine Generation, die ein verfallenes Handle
erkennbar macht statt ein falsches Bild zu zeigen. Abschnitt 14 bleibt
gewahrt, weil keine zweite Atlas-Engine entsteht; dreissig Zeilen Map auf
einem vorhandenen Dienst sind keine.

Der Preis gehoert dazu: eine Textur je Icon und Groesse statt einer
gemeinsamen Seite, also N Bildoperationen fuer N verschiedene Icons, die
nicht gebatcht werden. Bei Dutzenden Icons ist das tragbar, bei Tausenden
Glyphen waere es das nicht. Und weil das Uploadbudget pro gezeichnetem Frame
gilt, erscheint ein Schirm mit mehr neuen Icons als dem Budget erst ueber
mehrere Frames vollstaendig. Gemessen: acht pro Frame, also drei Frames fuer
zwanzig Icons. Ein Icon, dessen Upload abgelehnt wurde, zeichnet nichts und
holt es im naechsten gezeichneten Frame nach — es blitzt kein Platzhalter,
was fuer ein 16-Pixel-Symbol der schlechtere Fehler waere.

### Was `x/image/vector` nicht kann

Zwei Dinge, die dieser Abschnitt zunaechst verschwiegen hat, weil sie erst
beim Vermessen des Korpus sichtbar wurden. Beide sind der Grund, warum das
Paket groesser ist, als dieser Abschnitt vermuten liess.

**Der Rasterer fuellt, er strichelt nicht.** Die Haelfte des Korpus besteht
aus Strichen. Die Strichumrisse entstehen daher selbst, als Vereinigung
konvexer Stempel: ein Viereck je Segment, eine Verbindung je innerem Punkt,
eine Kappe je freiem Ende, alle gleich orientiert, sodass die
Nonzero-Fuellung genau ihre Vereinigung ist und keine Boolesche Operation
noetig wird. Ein versetzter Umriss wuerde sich selbst schneiden, sobald der
Pfad enger als die halbe Strichbreite dreht. Die Folge, die dazugehoert: die
Stempel ueberlappen, ein Strich laesst sich also nicht mit einer
transparenten Farbe zeichnen. Das kostet nichts, weil die Maske Deckung ist
und das Alpha in der Tinte steckt. Gestrichelt wird zur **Laufzeit**, nicht
vorab, weil der Umriss einer Kurve nur gegen eine Toleranz abgeflacht werden
kann und diese Toleranz von der Geraetepixelgroesse abhaengt, die der
Generator nicht kennt und der Cache-Miss sehr wohl.

**Der Rasterer kennt nur Nonzero.** Ein nennenswerter Teil des Korpus
deklariert `fill-rule="evenodd"`. Wie viele davon wirklich eine andere
Flaeche malen, ist zu **messen** und nicht zu schaetzen; gemessen waren es
45 von 211. Behoben wird das **offline**, indem Konturen nach Schachtelungs-
tiefe orientiert werden, und der Generator prueft jede Umorientierung gegen
die urspruengliche Evenodd-Fuellung, bevor er sie ausgibt.

Daraus das allgemeine Prinzip, das ueber diesen Fall hinausgeht und hier
festgehalten wird: **der Generator darf Arbeit tun, die der Frame-Pfad nicht
tun koennte, und alles, was der Frame-Pfad wiederholt tun muesste, gehoert in
den Generator.**

### Der Korpus, und was der Parser von ihm annimmt

Die Annahmen gehoeren aufgeschrieben, damit sie nachpruefbar sind, wenn der
Korpus sich aendert. Gemessen am Flowbite-Satz: **521 Dateien, 282 outline
und 239 solid**. Nur `<path>` und ein `<rect>`; kein `<g>`, keine
Transformationen, keine Verlaeufe. Keine Quadratiken. Der elliptische Bogen
ist mit Abstand der haeufigste Befehl und wird offline zu Kubiken. `solid/`
ist gefuellt, `outline/` gestrichelt mit Breite 2 und runden Kappen und
Verbindungen — wobei Miter der SVG-Standard ist und einige Pfade gar keine
Verbindung nennen, ihn also stillschweigend fordern.

Drei Ausnahmen, die je eine Regel im Code sind und ohne diese Notiz
willkuerlich aussaehen: ein abgerundetes `<rect>`, ein Pfad, der zugleich
gefuellt und gestrichelt ist, und ein Icon, dessen zweite Fuellung ein
literales Weiss ist und als Aussparung behandelt wird, weil ein Gift-Icon von
Bauart monochrom ist.

Icons gehen nicht durch `asset.Pipeline`. Die ist fuer Fotos gebaut, mit
Groessenleiter, Plattenspeicher-Cache und asynchroner Aufloesung; ein Icon
ist eingebettet, winzig und sofort verfuegbar, und ein einzelnes
Platzhalterbild fuer ein 16-Pixel-Symbol waere ein sichtbarer Fehler.

## 22. Weitere Bildformate

Abschnitt 12, Schritt 4 erlaubt weitere Formate ausdruecklich, sofern sie
ueber klar registrierte Decoder laufen. HEIF/HEIC ist weder RAW noch Video
und faellt daher nicht unter den Ausschluss in Abschnitt 14.

Eine Luecke im vorhandenen Vertrag muss dafuer geschlossen werden:
`asset/decode.go` liest die Orientierung nur fuer JPEG und nur aus EXIF. Ein
registrierter Decoder kann seine eigene Orientierung heute nicht melden.
HEIC traegt sie in den Boxen `irot` und `imir`. Es braucht daher eine
optionale Schnittstelle, ueber die ein Decoder seine Orientierung angibt.
Der Cache-Key traegt die Orientierung bereits, eine korrekt gemeldete
Orientierung wird also von selbst richtig zwischengespeichert.

`mimeByExtension` kennt nur `.jpg` und `.png` und wird ergaenzt. Da
Sniffing ohnehin Vorrang vor dem Medientyp hat, ist das die zweite
Verteidigungslinie und nicht die erste.

### Rechtlicher Hinweis zu HEIF/HEIC

Das Format ist unerwuenscht. Es ist mit diversen Patenten belastet, und die
unlizenzierte Nutzung kann durch einander widersprechende
Patentverwerterpools abgemahnt werden. Wir liefern deshalb **keine
Implementierung mit**, ermuntern niemanden zur Nutzung und raten im
Gegenteil davon ab. Wer die Unterstuetzung braucht, bindet sie selbst ein
und traegt die Verantwortung dafuer. Wir uebernehmen keine Haftung. Dies ist
keine Rechtsberatung.

## 23. Zweite Lieferung in pruefbaren Schritten

Wie Abschnitt 12: jeder Schritt endet in etwas Vorzeigbarem, und jeder
Schritt wird unabhaengig geprueft. Die sechs Review-Gates der ersten
Lieferung haben jeweils mindestens einen Blocker gefunden, den der
Implementierer nicht selbst gemeldet hatte. Das Verfahren bleibt.

### Schritt 6: Zwei Fehler im Scrollen

`pointer.dragged` wird nur beim Druck zurueckgesetzt, nicht beim Loslassen.
Touch entkommt dem, weil sein Slot beim Loslassen ganz genullt wird; die
Maus behaelt ihren Slot. Danach traegt jede Hover-Bewegung `Dragged`, der
Scroller haelt das fuer einen Zug, und der Rueckgabewert von
`StealPointer`, der genau das verhindern wuerde, wird verworfen.

Kein Test hat das gefunden, weil `Swipe` und `Fling` im Harness den
Touch-Pfad benutzen und kein Test die Maus **nach** dem Loslassen bewegt.
Der Regressionstest muss beides tun.

Dazu ein sichtbarer und greifbarer Scrollbalken fuer `ScrollView` und
`ImageGallery`. Die Geometrie liegt in `ScrollInfo` vollstaendig vor. Der
Griff muss `EventPointerMove` konsumieren, sonst nimmt ihm der Viewport
den Zug wieder ab.

Ergebnis: Galerie ohne klebendes Scrollen, mit Balken.

### Schritt 7: Fundamente

Abschnitt 18, Abschnitt 17, der Rune-Kanal aus Abschnitt 19 und
Abschnitt 20. Diese vier tragen alles Weitere und gehoeren deshalb vor die
Komponenten.

Ergebnis: scharfe Ausgabe auf 2x, eingebettete Schriften, stabile Goldens
fuer Anwender, ein Zeichen aus einer deutschen Tastatur kommt an, und ein
Beispiel schaltet zur Laufzeit zwischen hell und dunkel.

### Schritt 8: Eingabe und Icons

`ui.TextField` nach dem Muster von Button, Bildschirmtastatur, Icons.

Ergebnis: ein Eingabefeld mit Umlauten, Auswahl, Einfuegen und
Bildschirmtastatur im Kioskmodus.

### Schritt 9: Komponenten

Auswahl nach den Human Interface Guidelines: `ui.Toggle`, `ui.Slider`,
`ui.SegmentedControl` und `ui.ProgressBar`, jeweils nach dem Muster von
`ui.Button`. Navigation ueber eine TabBar, Kitchen-Sink-Demo.

Verbindlich fuer jedes dieser Steuerelemente:

- Die Trefferflaeche ist von der gezeichneten Groesse unabhaengig und misst
  mindestens 44 logische Pixel auf jeder Achse. Der Zielbildschirm ist ein
  Touchpanel; ein 31 Pixel hoher Schalter ist der richtige *Anblick* und ein
  schlechtes *Ziel*. Umgekehrt zeichnet ein Steuerelement nie ausserhalb seiner
  eigenen Grenzen: sichtbar, wo man nicht hinfassen kann, ist der gleiche
  Fehler andersherum.
- Ein ziehbarer Teil nimmt die Zeigererfassung und **konsumiert
  `EventPointerMove`**, sonst nimmt ihm ein umschliessender Viewport den Zug
  wieder ab — dieselbe Regel, die Schritt 6 fuer den Scrollbalken aufstellt.
- Der Ursprung einer Zieh-Geste wird **einmal beim Druck** festgehalten und nie
  aus dem aktuellen Wert neu berechnet. Ein Regler baut sich bei jeder
  Wertaenderung neu auf; die Inversion, die Review-Gate 7 im Scrollbalken
  gefunden hat, waere hier der Normalfall und kein Grenzfall.
- Eine Auswahl faellt an der **Position des Loslassens** und nicht an der des
  Drucks, und ein gezogenes Loslassen wird nicht abgelehnt. `DragSlop` sind
  8 logische Pixel, auf dem Zielpanel etwa 1,3 Millimeter: ein Finger, der
  beim Abheben abrollt, verliert die Auswahl sonst ganz. Umschliessende
  Scrollflaechen leiden nicht darunter, weil ein Scroller den Zeiger schon
  bei der Bewegung an sich nimmt und der Entzug das Steuerelement abbricht.
- Jede Animation hat eine Frist und endet. Die einzige Ausnahme ist die
  unbestimmte Fortschrittsanzeige, die sich aus ihrem eigenen Painter
  erneuert. Ihr Preis gehoert in die Dokumentation der Komponente, und zwar
  richtig formuliert: gift verwirft beim Zeichnen nichts, was ausserhalb des
  Sichtfelds liegt. Die Anzeige haelt das Geraet wach, solange sie
  **gemountet** ist — Wegscrollen aendert daran nichts, nur das Entfernen
  aus dem Baum.

### Schritt 10: Beispiele

Liquid Glass mit Animation, Persistenz mit Eingabefeld. HEIF nach
Abschnitt 22 ist unabhaengig und kann jederzeit vorgezogen werden.

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
