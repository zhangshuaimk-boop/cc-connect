import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { Badge } from './Badge';
import { Button } from './Button';
import { Card, StatCard } from './Card';
import { EmptyState } from './EmptyState';
import { Input, Textarea } from './Input';
import { Modal } from './Modal';

describe('ui components', () => {
  it('renders Badge variants with caller class names', () => {
    render(
      <Badge variant="success" className="custom-badge">
        Ready
      </Badge>,
    );

    const badge = screen.getByText('Ready');
    expect(badge.className).toContain('custom-badge');
    expect(badge.className).toContain('emerald');
  });

  it('disables Button while loading and renders the spinner', () => {
    const onClick = vi.fn();
    render(
      <Button loading onClick={onClick} variant="secondary" size="sm">
        Save
      </Button>,
    );

    const button = screen.getByRole('button', { name: /save/i });
    expect(button).toBeDisabled();
    expect(button.querySelector('svg')).not.toBeNull();
    fireEvent.click(button);
    expect(onClick).not.toHaveBeenCalled();
  });

  it('renders Card and StatCard content with optional hover/accent classes', () => {
    const { container } = render(
      <>
        <Card hover className="extra-card">
          Body
        </Card>
        <StatCard label="Projects" value={3} accent />
      </>,
    );

    expect(screen.getByText('Body')).toBeInTheDocument();
    expect(container.querySelector('.extra-card')?.className).toContain('hover:-translate-y-1');
    expect(screen.getByText('Projects')).toBeInTheDocument();
    expect(screen.getByText('3').className).toContain('text-accent');
  });

  it('connects Input and Textarea labels to visible controls', () => {
    render(
      <>
        <Input label="Project" defaultValue="demo" />
        <Textarea label="Prompt" defaultValue="hello" />
      </>,
    );

    expect(screen.getByDisplayValue('demo')).toBeInTheDocument();
    expect(screen.getByDisplayValue('hello')).toBeInTheDocument();
    expect(screen.getByText('Project')).toBeInTheDocument();
    expect(screen.getByText('Prompt')).toBeInTheDocument();
  });

  it('renders EmptyState with default and custom icons', () => {
    const CustomIcon = ({ className }: { className?: string }) => (
      <span data-testid="custom-icon" className={className} />
    );

    const { rerender } = render(<EmptyState message="No sessions" />);
    expect(screen.getByText('No sessions')).toBeInTheDocument();
    expect(document.querySelector('svg')).not.toBeNull();

    rerender(<EmptyState message="No skills" icon={CustomIcon} />);
    expect(screen.getByText('No skills')).toBeInTheDocument();
    expect(screen.getByTestId('custom-icon')).toBeInTheDocument();
  });

  it('renders Modal in a portal and closes from backdrop or close button', () => {
    const onClose = vi.fn();
    const { rerender } = render(
      <Modal open={false} onClose={onClose} title="Settings">
        Hidden
      </Modal>,
    );

    expect(screen.queryByText('Settings')).toBeNull();

    rerender(
      <Modal open onClose={onClose} title="Settings" className="settings-modal">
        <p>Visible body</p>
      </Modal>,
    );

    expect(screen.getByText('Settings')).toBeInTheDocument();
    expect(screen.getByText('Visible body')).toBeInTheDocument();
    expect(document.querySelector('.settings-modal')).not.toBeNull();

    fireEvent.click(screen.getByRole('presentation'));
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));

    expect(onClose).toHaveBeenCalledTimes(2);
  });
});
