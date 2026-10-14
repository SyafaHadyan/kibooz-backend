# Accessibility statement

Kibooz Backend is the API behind the Kibooz app for teachers and parents of kindergarten children. It has no screens of its own, so what it can do for accessibility is to make the app's job easier and to keep its own documentation usable. This page says what is promised, where the limits are and how to report a barrier.

## Commitment

- Every failure has a stable `errorCode` and a plain English `message`, so an app can show the person its own text, in their language and in the form that works for them, and never has to read out a code.
- A response carries data and text and never layout, color or markup, so the app decides how everything is presented.
- A refused request tells the app how long to wait with a `Retry-After` header, so the person can be told when to try again.
- The limits on uploads, such as the size and the pixels of a photo, are documented with their error codes, so an app can explain them before a person loses their work.
- Documentation changes are checked by CI, including a prose linter for the writing style.

## Supported environments

- The API is used over HTTPS by any client that can send JSON.
- The [API reference](https://docs.kibooz.syafahadyan.com) is built by Redocly from `openapi.yaml` and is meant for current desktop and mobile browsers.
- The Markdown pages of this repository are read on GitHub or in any text editor.

## Known limitations

- Nothing here has been audited for accessibility, and no conformance claim is made for any standard such as WCAG. The API has no interface to audit, and the documentation site has not been tested with assistive technology by the maintainers.
- How well the API reference works with a keyboard or a screen reader depends on the Redocly output, which this project does not control.
- Messages from the API are in English only. An app that serves people in another language has to map the error codes to its own text.
- Avatars and the guidance banners are images, and the API has no field for a text description of them. An app should describe an avatar with the name of the person and a banner with the title of its guide.
- A class video has no field for captions or a transcript, so a video with speech is only as accessible as the file the teacher uploads or the page that it links to.

## Report a barrier

Open an issue with the bug report form and say that it is an accessibility barrier. Describe what you were trying to do, where it went wrong and which tools you use, such as a browser and a screen reader. The maintainer labels it `accessibility` and aims to reply within a few business days.

A barrier in the Kibooz Android app itself belongs to the app project and not to this repository.
