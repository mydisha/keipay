/** Sets a cookie the way the server's Set-Cookie would (jsdom keeps it in document.cookie). */
export function setTestCookie(name: string, value: string) {
  document.cookie = `${name}=${value}; path=/`;
}

export function clearCookies() {
  for (const part of document.cookie.split(";")) {
    const name = part.split("=")[0]?.trim();
    if (name) document.cookie = `${name}=; path=/; max-age=0`;
  }
}
