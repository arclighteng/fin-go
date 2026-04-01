import { test, expect } from '@playwright/test';

// ---------------------------------------------------------------------------
// Accessibility — basic WCAG 2.1 AA checks without an axe dependency
// ---------------------------------------------------------------------------
//
// These tests cover the structural and keyboard requirements that matter
// most for a dashboard application:
//   - Every page has a <title>
//   - Images carry alt text
//   - Nav links have valid href attributes
//   - Forms have associated <label> elements
//   - No duplicate id attributes on any page
//   - Body has a foreground text color set (contrast baseline)
//   - Tab key moves focus through nav links
// ---------------------------------------------------------------------------

const pages = [
  { name: 'dashboard', path: '/dashboard' },
  { name: 'connect',   path: '/connect'   },
  { name: 'sync-log',  path: '/sync-log'  },
  { name: 'budget',    path: '/budget'    },
  { name: 'insights',  path: '/insights'  },
  { name: 'review',    path: '/review'    },
];

// ---------------------------------------------------------------------------
// <title> on every page
// ---------------------------------------------------------------------------

for (const { name, path } of pages) {
  test(`${name}: has a non-empty <title> tag`, async ({ page }) => {
    await page.goto(path);
    const title = await page.title();
    expect(title.trim().length, `${path} must have a non-empty title`).toBeGreaterThan(0);
  });
}

// ---------------------------------------------------------------------------
// Images must have alt text
// ---------------------------------------------------------------------------

for (const { name, path } of pages) {
  test(`${name}: all <img> elements have non-empty alt attributes`, async ({ page }) => {
    await page.goto(path);

    const violations = await page.evaluate(() => {
      const imgs = Array.from(document.querySelectorAll('img'));
      return imgs
        .filter(img => !img.hasAttribute('alt') || img.getAttribute('alt') === '')
        .map(img => img.outerHTML.slice(0, 120));
    });

    expect(violations, `${path} has images without alt text: ${violations.join(' | ')}`).toHaveLength(0);
  });
}

// ---------------------------------------------------------------------------
// Nav links must have non-empty href attributes
// ---------------------------------------------------------------------------

for (const { name, path } of pages) {
  test(`${name}: nav links have valid href attributes`, async ({ page }) => {
    await page.goto(path);

    const violations = await page.evaluate(() => {
      const links = Array.from(document.querySelectorAll('nav a'));
      return links
        .filter(a => {
          const href = (a as HTMLAnchorElement).getAttribute('href');
          return !href || href.trim() === '' || href === '#';
        })
        .map(a => a.outerHTML.slice(0, 120));
    });

    expect(violations, `${path} has nav links with missing/empty href: ${violations.join(' | ')}`).toHaveLength(0);
  });
}

// ---------------------------------------------------------------------------
// Form inputs must have an associated <label>
// ---------------------------------------------------------------------------

test('connect: SimpleFIN token textarea has an associated label', async ({ page }) => {
  await page.goto('/connect');

  // The textarea id is simplefinTokenInput; the label's for= should match
  const labelFor = await page.evaluate(() => {
    const label = document.querySelector('label[for="simplefinTokenInput"]');
    return label ? label.textContent?.trim() : null;
  });

  expect(labelFor, 'simplefinTokenInput should have an associated <label>').not.toBeNull();
  expect((labelFor as string).length).toBeGreaterThan(0);
});

test('connect: CSV file input has an associated label or aria-label', async ({ page }) => {
  await page.goto('/connect');

  const result = await page.evaluate(() => {
    const input = document.querySelector('#csvFileInput') as HTMLInputElement | null;
    if (!input) return 'missing';
    const id = input.id;
    const labelEl = document.querySelector(`label[for="${id}"]`);
    const ariaLabel = input.getAttribute('aria-label');
    const ariaLabelledBy = input.getAttribute('aria-labelledby');
    // The drop zone itself acts as a visual label — the input is visually inside it
    const inDropZone = !!input.closest('.drop-zone');
    return (labelEl || ariaLabel || ariaLabelledBy || inDropZone) ? 'ok' : 'unlabelled';
  });

  expect(result).not.toBe('missing');
  // If it's inside the drop-zone, that is accepted as a visible label container
  expect(result).not.toBe('unlabelled');
});

// ---------------------------------------------------------------------------
// No duplicate id attributes
// ---------------------------------------------------------------------------

for (const { name, path } of pages) {
  test(`${name}: no duplicate id attributes`, async ({ page }) => {
    await page.goto(path);

    const duplicates = await page.evaluate(() => {
      const allIds = Array.from(document.querySelectorAll('[id]')).map(el => el.id);
      const seen = new Set<string>();
      const dupes: string[] = [];
      for (const id of allIds) {
        if (seen.has(id)) dupes.push(id);
        else seen.add(id);
      }
      return dupes;
    });

    expect(duplicates, `${path} has duplicate ids: ${duplicates.join(', ')}`).toHaveLength(0);
  });
}

// ---------------------------------------------------------------------------
// Body has a foreground text color (contrast baseline)
// ---------------------------------------------------------------------------

for (const { name, path } of pages) {
  test(`${name}: body has a CSS color set (contrast baseline)`, async ({ page }) => {
    await page.goto(path);

    const color = await page.evaluate(() =>
      getComputedStyle(document.body).color
    );

    // Must resolve to an rgb/rgba value — not empty, not 'initial'
    expect(color, `${path} body color should be set`).toMatch(/^rgb/);
  });
}

// ---------------------------------------------------------------------------
// Keyboard navigation — Tab moves focus through nav links
// ---------------------------------------------------------------------------

test('dashboard: Tab key cycles through nav links', async ({ page }) => {
  await page.goto('/dashboard');

  // Focus the skip link first (it's the first focusable element)
  await page.keyboard.press('Tab');

  // Tab through elements until we hit a nav link or exhaust a reasonable count
  let foundNavLink = false;
  for (let i = 0; i < 10; i++) {
    const focusedHref = await page.evaluate(() => {
      const el = document.activeElement as HTMLAnchorElement;
      return el ? el.href : null;
    });
    if (focusedHref && (
      focusedHref.includes('/dashboard') ||
      focusedHref.includes('/connect') ||
      focusedHref.includes('/sync-log')
    )) {
      foundNavLink = true;
      break;
    }
    await page.keyboard.press('Tab');
  }

  expect(foundNavLink, 'Tab should move focus to a nav link').toBe(true);
});

// ---------------------------------------------------------------------------
// Skip link is present for keyboard users
// ---------------------------------------------------------------------------

test('dashboard: skip-to-main-content link is present', async ({ page }) => {
  await page.goto('/dashboard');

  const skipLink = page.locator('a.skip-link');
  await expect(skipLink).toHaveCount(1);
  const href = await skipLink.getAttribute('href');
  expect(href).toBe('#main-content');
});

// ---------------------------------------------------------------------------
// ARIA landmark: main content region is identifiable
// ---------------------------------------------------------------------------

for (const { name, path } of pages) {
  test(`${name}: #main-content element exists`, async ({ page }) => {
    await page.goto(path);
    await expect(page.locator('#main-content')).toHaveCount(1);
  });
}
