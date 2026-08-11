export function unicodeCharacterCount(value: string) {
  return Array.from(value).length;
}

export function assertPromptLength(prompt: string, model: string, maxCharacters: number) {
  const actualCharacters = unicodeCharacterCount(prompt);
  if (actualCharacters > maxCharacters) {
    throw new Error(
      `${model} 提示词最多 ${maxCharacters.toLocaleString()} 个 Unicode 字符，当前 ${actualCharacters.toLocaleString()} 个`,
    );
  }
}
