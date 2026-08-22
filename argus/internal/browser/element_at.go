package browser

const elementAtJavaScript = `(point) => {
  const selector = 'a,button,input,select,textarea,[role="button"],[role="link"],[role="checkbox"],[role="radio"],[role="menuitem"],[tabindex]';
  const hit = document.elementFromPoint(point[0], point[1]);
  const element = hit?.closest(selector);
  if (!element) return null;

  const labels = element.labels
    ? [...element.labels].map((label) => label.innerText.trim()).filter(Boolean)
    : [];
  const label = labels[0] || element.getAttribute('aria-label') || '';
  const name = label || element.innerText?.trim() || element.getAttribute('title') || element.getAttribute('placeholder') || '';
  const type = (element.getAttribute('type') || '').toLowerCase();
  const form = element.form;
  const method = (form?.method || 'get').toLowerCase();
  const words = (name || '').toLowerCase().split(/[^a-z]+/).filter(Boolean);
  const destructiveWords = new Set(['delete', 'remove', 'destroy', 'purchase', 'pay', 'checkout']);
  const destructive =
    element.matches('[data-danger],[data-destructive],[aria-label*="delete" i]') ||
    words.some((word) => destructiveWords.has(word));

  return {
    tag: element.tagName.toLowerCase(),
    role: element.getAttribute('role') || '',
    name: name.slice(0, 300),
    label: label.slice(0, 300),
    placeholder: (element.getAttribute('placeholder') || '').slice(0, 300),
    input_type: type,
    disabled: Boolean(element.disabled),
    checked: Boolean(element.checked),
    selected: element.tagName === 'SELECT' ? String(element.value || '').slice(0, 300) : '',
    mutating: destructive || type === 'submit' || Boolean(form && method !== 'get'),
    destructive
  };
}`
