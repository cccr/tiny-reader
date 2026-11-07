# Tiny Reader

A child types a word and presses Enter. Claude Haiku answers with a short, simple sentence
that uses the word. Typing the next letter hides the sentence and starts a new word.

```sh
cp .env.example .env   # put your Anthropic key in it
docker-compose up -d --build
```

Then open `http://<host>:3040` in the projector's browser.

Keys: letters type, Enter sends, Backspace deletes, Escape clears.

## Deploy to a server

```sh
cp deploy.env.example deploy.env   # set HOST=user@host
./deploy.sh
```

The script copies the files with `scp` and runs `docker-compose up -d --build` there.
The server needs a `.env` in the app folder (copied from here if one exists locally).

## Open it on an LG TV from Home Assistant

Copy `ha/tiny_reader_script.example.yaml` to a new HA script and set your TV's
`media_player` entity and the server address.
