// 単体テストは日本語の画面で確かめる。jsdom の navigator.languages は en-US なので、LocaleStore が日本語を選ぶように置き換える。
Object.defineProperty(navigator, 'languages', { value: ['ja-JP'], configurable: true });
