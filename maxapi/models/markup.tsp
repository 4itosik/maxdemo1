namespace MaxBotApi;

/** Элемент разметки текста сообщения */
@discriminator("type")
model MarkupElement {
  /** Индекс начала элемента разметки в тексте. Нумерация с нуля */
  from: int32;

  /** Длина элемента разметки */
  length: int32;
}

/** Представляет **жирный** текст */
model StrongMarkup extends MarkupElement {
  type: "strong";
}

/** Представляет *курсив* */
model EmphasizedMarkup extends MarkupElement {
  type: "emphasized";
}

/** Представляет `моноширинный` или блок ```код``` в тексте */
model MonospacedMarkup extends MarkupElement {
  type: "monospaced";
}

/** Представляет ссылку в тексте */
model LinkMarkup extends MarkupElement {
  type: "link";

  /** URL ссылки */
  @minLength(1)
  @maxLength(2048)
  url: string;
}

/** Представляет ~зачёркнутый~ текст */
model StrikethroughMarkup extends MarkupElement {
  type: "strikethrough";
}

/** Представляет подчёркнутый текст */
model UnderlineMarkup extends MarkupElement {
  type: "underline";
}

/** Представляет упоминание пользователя в тексте. Упоминание может быть как по имени пользователя, так и по ID, если у пользователя нет имени */
model UserMentionMarkup extends MarkupElement {
  type: "user_mention";

  /** `@username` упомянутого пользователя */
  user_link?: string | null;

  /** ID упомянутого пользователя без имени */
  user_id?: int64 | null;
}

/** Представляет заголовок */
model HeadingMarkup extends MarkupElement {
  type: "heading";
}

/** Представляет выделенный текст */
model HighlightedMarkup extends MarkupElement {
  type: "highlighted";
}

/** Представляет цитату */
model QuoteMarkup extends MarkupElement {
  type: "quote";
}
