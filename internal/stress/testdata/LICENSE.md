# Third party font used by the tests of gift/internal/text

## `Roboto-Regular.ttf`

Source: <https://fonts.google.com/specimen/Roboto>

Copyright 2011 Google Inc. All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use
this file except in compliance with the License. You may obtain a copy of the
License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed
under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
CONDITIONS OF ANY KIND, either express or implied. See the License for the
specific language governing permissions and limitations under the License.

The file is byte identical to the copy shipped in the test data of
`github.com/hajimehoshi/ebiten/v2@v2.10.1` (`text/v2/testdata`), which is where
it was taken from. It is used because it carries the OpenType layout tables the
tests need: a GPOS kerning feature and the standard `liga` feature, neither of
which the Go fonts in `golang.org/x/image/font/gofont` contain. Nothing outside
the test binary depends on it; gift ships no font.
