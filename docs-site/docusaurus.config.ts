import { themes as prismThemes } from 'prism-react-renderer';
import type { Config } from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

const config: Config = {
  title: 'Subs-Check⁺ PRO',
  tagline: '高性能代理订阅检测工具 - 支持测活、测速、媒体解锁',
  favicon: 'img/favicon.ico',

  future: { v4: true },

  url: 'https://sinspired.github.io',
  baseUrl: '/subs-check-pro',
  organizationName: 'sinspired',
  projectName: 'subs-check-pro',

  trailingSlash: false,

  onBrokenLinks: 'throw',

  i18n: {
    defaultLocale: 'zh-Hans',
    locales: ['zh-Hans'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          sidebarPath: './sidebars.ts',
          editUrl: 'https://github.com/sinspired/subs-check-pro/edit/main/docs-site/',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    image: 'img/Subs-Check-PRO_OG.png',
    colorMode: { respectPrefersColorScheme: true },
    navbar: {
      title: 'Subs-Check⁺ PRO',
      logo: { alt: 'Subs-Check Logo', src: 'img/favicon.ico' },
      items: [
        { type: 'docSidebar', sidebarId: 'mainSidebar', position: 'left', label: '文档' },
        {
          href: 'https://github.com/sinspired/subs-check-pro/wiki',
          position: 'left',
          label: 'Wiki',
        },
        {
          href: 'https://t.me/subs_check_pro',
          position: 'right',
          className: 'header-telegram-link header-link',
          'aria-label': 'Telegram group',
        },
        {
          href: 'https://github.com/sinspired/subs-check-pro',
          position: 'right',
          className: 'header-github-link header-link',
          'aria-label': 'GitHub repository',
        },
      ],
    },
    footer: {
      style: 'light',
      links: [
        {
          title: '产品',
          items: [
            {
              label: '功能特性',
              href: 'https://github.com/sinspired/subs-check-pro#-特性',
            },
            {
              label: '快速开始',
              href: 'https://github.com/sinspired/subs-check-pro#-快速开始',
            },
            {
              label: '发行版',
              href: 'https://github.com/sinspired/subs-check-pro/releases',
            },
          ],
        },
        {
          title: '社区',
          items: [
            {
              label: 'GitHub',
              href: 'https://github.com/sinspired/subs-check-pro',
            },
            {
              label: 'Telegram 群组',
              href: 'https://t.me/subs_check_pro',
            },
            {
              label: 'Telegram 频道',
              href: 'https://t.me/sinspired_ai',
            },
          ],
        },
      ],
      copyright: `Made by <a href="https://github.com/sinspired" target="_blank" rel="noopener noreferrer">sinspired</a> · © ${new Date().getFullYear()} Subs-Check⁺ PRO`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
    },
  } satisfies Preset.ThemeConfig,
};

export default config;