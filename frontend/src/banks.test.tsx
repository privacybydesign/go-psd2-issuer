import { describe, it, expect, beforeAll, beforeEach, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { BankList, FailedBanksAlert } from './components';
import BanksPage from './pages/Banks';
import { BankStatus, MAX_BANKS } from './types';
import * as api from './api';
import i18n from './i18n';

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  getStatus: vi.fn(),
  addBank: vi.fn(),
  finishConnecting: vi.fn(),
  deleteSession: vi.fn(),
}));

const banks = (n: number): BankStatus[] =>
  Array.from({ length: n }, (_, i) => ({ provider: `BANK${i + 1}`, status: 'pending' }));

const renderBanksPage = () =>
  render(
    <MemoryRouter initialEntries={['/en/banks']}>
      <Routes>
        <Route path="/en/banks" element={<BanksPage />} />
        <Route path="/en/progress" element={<p>progress page</p>} />
        <Route path="/en/error" element={<p>error page</p>} />
      </Routes>
    </MemoryRouter>,
  );

describe('BankList', () => {
  beforeAll(async () => {
    await i18n.changeLanguage('en');
  });

  it('lists every bank with its status', () => {
    render(
      <BankList
        banks={[
          { provider: 'RABONL2U', status: 'ready' },
          { provider: 'ABNANL2A', status: 'failed' },
          { provider: 'INGBNL2A', status: 'pending' },
        ]}
      />,
    );
    expect(screen.getByText('RABONL2U')).toBeInTheDocument();
    expect(screen.getByText('ABNANL2A')).toBeInTheDocument();
    expect(screen.getByText('INGBNL2A')).toBeInTheDocument();
    expect(screen.getByText('Failed')).toBeInTheDocument();
    expect(screen.getByText('Connected')).toBeInTheDocument();
  });
});

describe('FailedBanksAlert', () => {
  beforeAll(async () => {
    await i18n.changeLanguage('en');
  });

  it('renders nothing when every bank succeeded', () => {
    const { container } = render(<FailedBanksAlert banks={[{ provider: 'RABONL2U', status: 'ready' }]} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('names the banks that failed', () => {
    render(
      <FailedBanksAlert
        banks={[
          { provider: 'RABONL2U', status: 'ready' },
          { provider: 'ABNANL2A', status: 'failed' },
        ]}
      />,
    );
    expect(screen.getByRole('alert')).toHaveTextContent('ABNANL2A');
    expect(screen.getByRole('alert')).not.toHaveTextContent('RABONL2U');
  });
});

describe('BanksPage', () => {
  beforeAll(async () => {
    await i18n.changeLanguage('en');
  });

  beforeEach(() => {
    vi.resetAllMocks();
  });

  it('connects another bank', async () => {
    vi.mocked(api.getStatus).mockResolvedValue({ status: 'awaiting_consent', banks: banks(1) });
    vi.mocked(api.addBank).mockResolvedValue({ url: 'https://app.enablenow.nl/?client=x' });
    const assign = vi.fn();
    vi.stubGlobal('location', { ...window.location, assign });

    renderBanksPage();
    expect(await screen.findByText('BANK1')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Connect another bank' }));

    await waitFor(() => expect(assign).toHaveBeenCalledWith('https://app.enablenow.nl/?client=x'));
    vi.unstubAllGlobals();
  });

  it('continues to the analysis', async () => {
    vi.mocked(api.getStatus).mockResolvedValue({ status: 'awaiting_consent', banks: banks(2) });
    vi.mocked(api.finishConnecting).mockResolvedValue({ status: 'awaiting_transactions' });

    renderBanksPage();
    expect(await screen.findByText('BANK2')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

    expect(await screen.findByText('progress page')).toBeInTheDocument();
    expect(api.finishConnecting).toHaveBeenCalledOnce();
  });

  it('cannot add more than the maximum number of banks', async () => {
    vi.mocked(api.getStatus).mockResolvedValue({ status: 'awaiting_consent', banks: banks(MAX_BANKS) });

    renderBanksPage();
    expect(await screen.findByText(`BANK${MAX_BANKS}`)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Connect another bank' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Continue' })).toBeEnabled();
  });

  it('goes to the progress page when connecting is already done', async () => {
    vi.mocked(api.getStatus).mockResolvedValue({ status: 'categorizing', banks: banks(1) });

    renderBanksPage();
    expect(await screen.findByText('progress page')).toBeInTheDocument();
  });

  it('shows an error without a connected bank', async () => {
    vi.mocked(api.getStatus).mockResolvedValue({ status: 'awaiting_consent' });

    renderBanksPage();
    expect(await screen.findByText('error page')).toBeInTheDocument();
  });
});
