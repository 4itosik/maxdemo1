namespace MaxBotApi;

/** Элемент разметки текста сообщения */
@discriminator("type")
model MarkupElement {
  /** Тип элемента разметки. 64 — в оригинале лимита нет; по аналогии с официальным лимитом коротких имён (BotCommand.name) */
  @maxLength(64)
  @pattern("^[a-z_]+$")
  type: string;

  /** Индекс начала элемента разметки в тексте. Нумерация с нуля */
  @minValue(0)
  @maxValue(2147483647)
  from: int32;

  /** Длина элемента разметки */
  @minValue(0)
  @maxValue(2147483647)
  length: int32;
}

/** Представляет **жирный** текст */
model StrongMarkup extends MarkupElement {
  /** Тип элемента разметки: `strong` */
  type: "strong";
}

/** Представляет *курсив* */
model EmphasizedMarkup extends MarkupElement {
  /** Тип элемента разметки: `emphasized` */
  type: "emphasized";
}

/** Представляет `моноширинный` или блок ```код``` в тексте */
model MonospacedMarkup extends MarkupElement {
  /** Тип элемента разметки: `monospaced` */
  type: "monospaced";
}

/** Представляет ссылку в тексте */
model LinkMarkup extends MarkupElement {
  /** Тип элемента разметки: `link` */
  type: "link";

  /** URL ссылки. 2048 — значение из официальной схемы MAX */
  @minLength(1)
  @maxLength(2048)
  @pattern("^[\\w\\W]{1,2048}$")
  url: string;
}

/** Представляет ~зачёркнутый~ текст */
model StrikethroughMarkup extends MarkupElement {
  /** Тип элемента разметки: `strikethrough` */
  type: "strikethrough";
}

/** Представляет подчёркнутый текст */
model UnderlineMarkup extends MarkupElement {
  /** Тип элемента разметки: `underline` */
  type: "underline";
}

/** Представляет упоминание пользователя в тексте. Упоминание может быть как по имени пользователя, так и по ID, если у пользователя нет имени */
model UserMentionMarkup extends MarkupElement {
  /** Тип элемента разметки: `user_mention` */
  type: "user_mention";

  /** `@username` упомянутого пользователя */
  @maxLength(255)
  @pattern("^[\\w\\W]{0,255}$")
  user_link?: string | null;

  /** ID упомянутого пользователя без имени */
  @minValue(0)
  @maxValue(9007199254740991)
  user_id?: int64 | null;
}

/** Представляет заголовок */
model HeadingMarkup extends MarkupElement {
  /** Тип элемента разметки: `heading` */
  type: "heading";
}

/** Представляет выделенный текст */
model HighlightedMarkup extends MarkupElement {
  /** Тип элемента разметки: `highlighted` */
  type: "highlighted";
}

/** Представляет цитату */
model QuoteMarkup extends MarkupElement {
  /** Тип элемента разметки: `quote` */
  type: "quote";
}
